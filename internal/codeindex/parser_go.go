package codeindex

import (
	"bytes"
	"context"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"strconv"
	"strings"
)

func parseGo(ctx context.Context, f *File, src []byte) error {
	fs := token.NewFileSet()
	root, err := parser.ParseFile(fs, f.Path, src, parser.AllErrors)
	if err != nil {
		return err
	}
	f.Package = root.Name.Name
	namespaces := goImports(f, root)
	ast.Inspect(root, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			f.Identifiers = append(f.Identifiers, id.Name)
		}
		return true
	})
	for _, d := range root.Decls {
		if err := ctx.Err(); err != nil {
			return err
		}
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		s, err := goSymbol(fs, fn, src, namespaces)
		if err != nil {
			return err
		}
		f.Symbols = append(f.Symbols, s)
	}
	return nil
}
func goImports(f *File, root *ast.File) map[string]bool {
	namespaces := map[string]bool{}
	for _, im := range root.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		alias := path.Base(p)
		if im.Name != nil {
			alias = im.Name.Name
		}
		f.Imports = append(f.Imports, Import{Alias: alias, Path: p})
		if alias != "_" && alias != "." {
			namespaces[alias] = true
		}
	}
	return namespaces
}
func goSymbol(fs *token.FileSet, fn *ast.FuncDecl, src []byte, namespaces map[string]bool) (Symbol, error) {
	s := Symbol{Name: fn.Name.Name, Qualified: fn.Name.Name, Kind: "function", Exported: ast.IsExported(fn.Name.Name), StartLine: fs.Position(fn.Pos()).Line, EndLine: fs.Position(fn.End()).Line}
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		s.Kind = "method"
		s.Qualified = qualify(goReceiver(fn.Recv.List[0].Type), s.Name)
	}
	var b bytes.Buffer
	signature := *fn.Type
	// Existing graph rules consume `(params) results`, including generic functions.
	signature.TypeParams = nil
	if err := format.Node(&b, fs, &signature); err != nil {
		return s, err
	}
	s.Signature = strings.TrimPrefix(b.String(), "func")
	if fn.Body == nil {
		return s, nil
	}
	body := src[fs.Position(fn.Body.Pos()).Offset:fs.Position(fn.Body.End()).Offset]
	s.Fingerprint = goFingerprint(body)
	bound := goBindings(fn)
	goCalls(fs, fn.Body, bound, namespaces, &s)
	return s, nil
}
func goBindFields(fields *ast.FieldList, bound map[string]bool) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			bound[name.Name] = true
		}
	}
}
func goBindExprs(exprs []ast.Expr, bound map[string]bool) {
	for _, e := range exprs {
		if id, ok := e.(*ast.Ident); ok {
			bound[id.Name] = true
		}
	}
}
func goBindings(fn *ast.FuncDecl) map[string]bool {
	bound := map[string]bool{}
	goBindFields(fn.Recv, bound)
	goBindFields(fn.Type.Params, bound)
	goBindFields(fn.Type.Results, bound)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				goBindExprs(n.Lhs, bound)
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				goBindExprs([]ast.Expr{n.Key, n.Value}, bound)
			}
		case *ast.TypeSpec:
			bound[n.Name.Name] = true
		case *ast.ValueSpec:
			for _, id := range n.Names {
				bound[id.Name] = true
			}
		}
		return true
	})
	return bound
}
func goCalls(fs *token.FileSet, body *ast.BlockStmt, bound, namespaces map[string]bool, s *Symbol) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		call := goCallTarget(c.Fun, bound, namespaces)
		call.Line = fs.Position(c.Pos()).Line
		if call.Name != "" {
			s.Calls = append(s.Calls, call)
		} else {
			s.Unresolved++
		}
		return true
	})
}
func goCallTarget(target ast.Expr, bound, namespaces map[string]bool) Call {
	// Generic instantiation is still a lexical call.
	switch t := target.(type) {
	case *ast.IndexExpr:
		target = t.X
	case *ast.IndexListExpr:
		target = t.X
	}
	switch t := target.(type) {
	case *ast.Ident:
		if !bound[t.Name] {
			return Call{Name: t.Name}
		}
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok && namespaces[id.Name] && !bound[id.Name] {
			return Call{Name: t.Sel.Name, Qualifier: id.Name}
		}
	}
	return Call{}
}
func goReceiver(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return goReceiver(e.X)
	case *ast.IndexExpr:
		return goReceiver(e.X)
	case *ast.IndexListExpr:
		return goReceiver(e.X)
	}
	return ""
}
func goFingerprint(src []byte) string {
	var s scanner.Scanner
	fs := token.NewFileSet()
	s.Init(fs.AddFile("", -1, len(src)), src, nil, 0)
	var b strings.Builder
	count := 0
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON {
			continue
		}
		if tok != token.LBRACE && tok != token.RBRACE {
			count++
		}
		b.WriteString(tok.String())
		b.WriteByte(':')
		b.WriteString(lit)
		b.WriteByte(0)
	}
	if count < 20 {
		return ""
	}
	return fingerprint(b.String())
}
