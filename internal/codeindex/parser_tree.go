package codeindex

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func parseTree(ctx context.Context, f *File, src []byte) error {
	p := sitter.NewParser()
	defer p.Close()
	var lang *sitter.Language
	switch f.Language {
	case "javascript":
		lang = sitter.NewLanguage(javascript.Language())
	case "typescript":
		if filepath.Ext(f.Path) == ".tsx" {
			lang = sitter.NewLanguage(typescript.LanguageTSX())
		} else {
			lang = sitter.NewLanguage(typescript.LanguageTypescript())
		}
	case "python":
		lang = sitter.NewLanguage(python.Language())
	}
	if err := p.SetLanguage(lang); err != nil {
		return err
	}
	tree := p.ParseWithOptions(func(offset int, _ sitter.Point) []byte {
		if offset >= len(src) {
			return nil
		}
		return src[offset:]
	}, nil, &sitter.ParseOptions{ProgressCallback: func(sitter.ParseState) bool {
		return ctx.Err() != nil
	}})
	if tree == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("parse %s: no syntax tree", f.Path)
	}
	defer tree.Close()
	root := tree.RootNode()
	if root.HasError() {
		return fmt.Errorf("parse %s: invalid %s syntax", f.Path, f.Language)
	}
	x := treeExtractor{file: f, src: src, namespaces: map[string]bool{}}
	x.fileMetadata(root)
	x.extract(root, "", nil)
	x.explicitExports(root)
	return ctx.Err()
}

func localImport(n *sitter.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if callable(p) || p.Kind() == "class_definition" {
			return true
		}
	}
	return false
}
func (x *treeExtractor) fileMetadata(root *sitter.Node) {
	walkTree(root, func(n *sitter.Node) bool {
		if n.Kind() == "identifier" || n.Kind() == "property_identifier" {
			x.file.Identifiers = append(x.file.Identifiers, x.text(n))
		}
		if (n.Kind() == "import_statement" || n.Kind() == "import_from_statement") && !localImport(n) {
			x.imports(n)
		}
		return true
	})
	for _, im := range x.file.Imports {
		if im.Name == "" {
			x.namespaces[im.Alias] = true
		}
	}
}
func (x *treeExtractor) explicitExports(root *sitter.Node) {
	// Explicit same-name exports; re-exports require module linking and are not guessed.
	walkTree(root, func(n *sitter.Node) bool {
		if n.Kind() == "export_statement" && n.ChildByFieldName("source") == nil {
			walkTree(n, x.exportSpecifier)
		}
		return true
	})
}
func (x *treeExtractor) exportSpecifier(n *sitter.Node) bool {
	if n.Kind() != "export_specifier" {
		return true
	}
	name := x.text(n.ChildByFieldName("name"))
	alias := x.text(n.ChildByFieldName("alias"))
	if alias != "" && alias != name {
		return true
	}
	for i := range x.file.Symbols {
		if x.file.Symbols[i].Qualified == name {
			x.file.Symbols[i].Exported = true
		}
	}
	return true
}

type treeExtractor struct {
	file       *File
	src        []byte
	namespaces map[string]bool
}

func (x *treeExtractor) text(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(x.src)
}
func walkTree(n *sitter.Node, visit func(*sitter.Node) bool) {
	if n == nil || !visit(n) {
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkTree(n.NamedChild(i), visit)
	}
}
func callable(n *sitter.Node) bool {
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration", "function_expression", "generator_function", "arrow_function", "method_definition", "function_definition":
		return true
	}
	return false
}
func (x *treeExtractor) functionName(n *sitter.Node) string {
	p := n.Parent()
	if p != nil && p.Kind() == "variable_declarator" {
		return x.text(p.ChildByFieldName("name"))
	}
	if p != nil && (p.Kind() == "public_field_definition" || p.Kind() == "field_definition") {
		return x.text(p.ChildByFieldName("property"))
	}
	if name := n.ChildByFieldName("name"); name != nil {
		return x.text(name)
	}
	if p == nil {
		return ""
	}
	switch p.Kind() {
	case "variable_declarator":
		return x.text(p.ChildByFieldName("name"))
	case "pair":
		return strings.Trim(x.text(p.ChildByFieldName("key")), "\"'")
	case "assignment":
		if left := p.ChildByFieldName("left"); left != nil && left.Kind() == "identifier" {
			return x.text(left)
		}
	}
	return ""
}
func (x *treeExtractor) extract(n *sitter.Node, scope string, inherited map[string]bool) {
	if n == nil {
		return
	}
	if n.Kind() == "class_declaration" || n.Kind() == "class_definition" || n.Kind() == "class" {
		scope = qualify(scope, x.text(n.ChildByFieldName("name")))
	}
	if n.Kind() == "object" && n.Parent() != nil && n.Parent().Kind() == "variable_declarator" {
		scope = qualify(scope, x.text(n.Parent().ChildByFieldName("name")))
	}
	if callable(n) {
		name := x.functionName(n)
		body := n.ChildByFieldName("body")
		bound := cloneBindings(inherited)
		x.bindPattern(n.ChildByFieldName("parameters"), bound)
		x.bindPattern(n.ChildByFieldName("parameter"), bound)
		x.localBindings(body, bound)
		if name != "" {
			s := x.treeSymbol(n, scope, name)
			x.functionCalls(body, bound, &s)
			x.file.Symbols = append(x.file.Symbols, s)
			scope = s.Qualified
		}
		// Anonymous functions are not attributed to their enclosing function.
		for i := uint(0); i < n.NamedChildCount(); i++ {
			x.extract(n.NamedChild(i), scope, bound)
		}
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		x.extract(n.NamedChild(i), scope, inherited)
	}
}
func (x *treeExtractor) treeSymbol(n *sitter.Node, scope, name string) Symbol {
	return Symbol{Name: name, Qualified: qualify(scope, name), Kind: treeSymbolKind(n), Exported: x.declarationExported(n, scope), StartLine: int(n.StartPosition().Row) + 1, EndLine: int(n.EndPosition().Row) + 1, Signature: x.treeSignature(n), Fingerprint: x.bodyFingerprint(n.ChildByFieldName("body"))}
}
func treeSymbolKind(n *sitter.Node) string {
	if n.Kind() == "method_definition" {
		return "method"
	}
	p := n.Parent()
	if p != nil && p.Kind() == "decorated_definition" {
		p = p.Parent()
	}
	if p == nil {
		return "function"
	}
	if p.Kind() == "public_field_definition" || p.Kind() == "field_definition" {
		return "method"
	}
	if n.Kind() == "function_definition" && p.Parent() != nil && p.Parent().Kind() == "class_definition" {
		return "method"
	}
	return "function"
}
func (x *treeExtractor) treeSignature(n *sitter.Node) string {
	sig := x.text(n.ChildByFieldName("parameters"))
	if sig == "" {
		sig = "(" + x.text(n.ChildByFieldName("parameter")) + ")"
	}
	result := x.text(n.ChildByFieldName("return_type"))
	if result != "" && x.file.Language == "python" {
		sig += " -> "
	}
	return compact(sig + result)
}
func (x *treeExtractor) declarationExported(n *sitter.Node, scope string) bool {
	if scope != "" {
		return false
	}
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "export_statement" {
			return !hasChildKind(p, "default")
		}
		if callable(p) {
			break
		}
	}
	return x.file.Language == "python"
}
func hasChildKind(n *sitter.Node, kind string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		if n.Child(i).Kind() == kind {
			return true
		}
	}
	return false
}
func (x *treeExtractor) functionCalls(body *sitter.Node, bound map[string]bool, s *Symbol) {
	walkTree(body, func(n *sitter.Node) bool {
		if callable(n) {
			return false
		}
		if n.Kind() != "call_expression" && n.Kind() != "call" {
			return true
		}
		target := n.ChildByFieldName("function")
		if target == nil {
			return true
		}
		call := x.treeCallTarget(target, bound)
		call.Line = int(n.StartPosition().Row) + 1
		if call.Name != "" {
			s.Calls = append(s.Calls, call)
		} else {
			s.Unresolved++
		}
		return true
	})
}
func (x *treeExtractor) treeCallTarget(n *sitter.Node, bound map[string]bool) Call {
	if n.Kind() == "identifier" && !bound[x.text(n)] {
		return Call{Name: x.text(n)}
	}
	if n.Kind() != "member_expression" && n.Kind() != "attribute" {
		return Call{}
	}
	object := n.ChildByFieldName("object")
	alias := x.text(object)
	if object == nil || object.Kind() != "identifier" || !x.namespaces[alias] || bound[alias] {
		return Call{}
	}
	prop := n.ChildByFieldName("property")
	if prop == nil {
		prop = n.ChildByFieldName("attribute")
	}
	return Call{Name: x.text(prop), Qualifier: alias}
}
func cloneBindings(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (x *treeExtractor) bindPattern(n *sitter.Node, bound map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "identifier", "shorthand_property_identifier_pattern":
		bound[x.text(n)] = true
		return
	case "type_annotation", "type_identifier":
		return
	case "pair_pattern":
		x.bindPattern(n.ChildByFieldName("value"), bound)
		return
	case "assignment_pattern", "default_parameter", "typed_default_parameter":
		x.bindPattern(n.ChildByFieldName("left"), bound)
		x.bindPattern(n.ChildByFieldName("name"), bound)
		return
	case "required_parameter", "optional_parameter":
		x.bindPattern(n.ChildByFieldName("pattern"), bound)
		return
	case "typed_parameter":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			if c.Kind() == "identifier" || c.Kind() == "list_splat_pattern" || c.Kind() == "dictionary_splat_pattern" {
				x.bindPattern(c, bound)
			}
		}
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		x.bindPattern(n.NamedChild(i), bound)
	}
}
func (x *treeExtractor) localBindings(body *sitter.Node, bound map[string]bool) {
	walkTree(body, func(n *sitter.Node) bool {
		if callable(n) {
			return false
		}
		switch n.Kind() {
		case "import_statement", "import_from_statement":
			local := treeExtractor{file: &File{}, src: x.src}
			local.pythonImports(n)
			for _, im := range local.file.Imports {
				bound[im.Alias] = true
			}
			return false
		case "class_definition", "class_declaration":
			x.bindPattern(n.ChildByFieldName("name"), bound)
			return false
		case "variable_declarator":
			v := n.ChildByFieldName("value")
			if v == nil || !callable(v) {
				x.bindPattern(n.ChildByFieldName("name"), bound)
			}
		case "assignment", "augmented_assignment", "named_expression", "assignment_expression", "augmented_assignment_expression":
			x.bindPattern(n.ChildByFieldName("left"), bound)
			x.bindPattern(n.ChildByFieldName("name"), bound)
		case "for_statement", "for_in_statement", "for_in_clause":
			x.bindPattern(n.ChildByFieldName("left"), bound)
		case "catch_clause":
			x.bindPattern(n.ChildByFieldName("parameter"), bound)
		case "as_pattern":
			x.bindPattern(n.ChildByFieldName("alias"), bound)
		}
		return true
	})
}
func (x *treeExtractor) bodyFingerprint(n *sitter.Node) string {
	var b strings.Builder
	count := 0
	var visit func(*sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil || n.Kind() == "comment" || n.Kind() == ";" {
			return
		}
		if n.ChildCount() == 0 {
			b.WriteString(n.Kind())
			b.WriteByte(':')
			b.WriteString(x.text(n))
			b.WriteByte(0)
			if n.Kind() != "{" && n.Kind() != "}" && n.Kind() != ";" {
				count++
			}
			return
		}
		b.WriteString(n.Kind())
		b.WriteByte('(')
		for i := uint(0); i < n.ChildCount(); i++ {
			visit(n.Child(i))
		}
		b.WriteByte(')')
	}
	visit(n)
	if count < 20 {
		return ""
	}
	return fingerprint(b.String())
}
func (x *treeExtractor) imports(n *sitter.Node) {
	if x.file.Language == "python" {
		x.pythonImports(n)
		return
	}
	source := n.ChildByFieldName("source")
	if source == nil {
		return
	}
	p := unquote(x.text(source))
	walkTree(n, func(c *sitter.Node) bool {
		switch c.Kind() {
		case "namespace_import":
			if c.NamedChildCount() > 0 {
				x.file.Imports = append(x.file.Imports, Import{Alias: x.text(c.NamedChild(c.NamedChildCount() - 1)), Path: p})
			}
			return false
		case "import_specifier":
			name := x.text(c.ChildByFieldName("name"))
			alias := x.text(c.ChildByFieldName("alias"))
			if alias == "" {
				alias = name
			}
			x.file.Imports = append(x.file.Imports, Import{Alias: alias, Name: name, Path: p})
			return false
		case "import_clause":
			for i := uint(0); i < c.NamedChildCount(); i++ {
				d := c.NamedChild(i)
				if d.Kind() == "identifier" {
					x.file.Imports = append(x.file.Imports, Import{Alias: x.text(d), Name: "default", Path: p})
				}
			}
		}
		return true
	})
}
func unquote(s string) string {
	if value, err := strconv.Unquote(s); err == nil {
		return value
	}
	return strings.Trim(s, "\"'")
}
func (x *treeExtractor) pythonImports(n *sitter.Node) {
	module := n.ChildByFieldName("module_name")
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if module != nil && c.Id() == module.Id() {
			continue
		}
		if c.Kind() != "dotted_name" && c.Kind() != "aliased_import" {
			continue
		}
		name := x.text(c)
		alias := ""
		if c.Kind() == "aliased_import" {
			name = x.text(c.ChildByFieldName("name"))
			alias = x.text(c.ChildByFieldName("alias"))
		}
		if module != nil {
			if alias == "" {
				alias = name
			}
			x.file.Imports = append(x.file.Imports, Import{Alias: alias, Path: x.text(module), Name: name})
		} else {
			if alias == "" {
				alias = strings.Split(name, ".")[0]
			}
			x.file.Imports = append(x.file.Imports, Import{Alias: alias, Path: name})
		}
	}
}
