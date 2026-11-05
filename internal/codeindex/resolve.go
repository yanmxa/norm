package codeindex

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

type resolver struct {
	files   map[string]File
	symbols map[string][]Symbol
	module  string
}

func resolve(root string, index *Index) {
	r := resolver{files: map[string]File{}, symbols: map[string][]Symbol{}}
	if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		r.module = modfile.ModulePath(data)
	}
	for _, file := range index.Files {
		r.files[file.Path] = file
		r.symbols[file.Path] = file.Symbols
	}
	seen := map[Edge]bool{}
	for _, file := range index.Files {
		for _, symbol := range file.Symbols {
			index.Unresolved += symbol.Unresolved
			for _, call := range symbol.Calls {
				target, ok := r.callTarget(file, symbol, call)
				if !ok {
					index.Unresolved++
					continue
				}
				edge := Edge{Source: symbol.ID, Target: target.ID, Kind: "calls", Line: call.Line}
				if !seen[edge] {
					index.Edges = append(index.Edges, edge)
					seen[edge] = true
				}
			}
		}
	}
	sort.Slice(index.Edges, func(i, j int) bool {
		a, b := index.Edges[i], index.Edges[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Line < b.Line
	})
}

func (r *resolver) callTarget(file File, caller Symbol, call Call) (Symbol, bool) {
	if call.Qualifier == "" {
		// Prefer the nearest lexical declaration. Ambiguity never falls through to
		// a similarly named symbol elsewhere in the repository.
		if matches := r.lexical(file, caller, call.Name); len(matches) > 0 {
			return uniqueSymbol(matches)
		}
	}
	for _, imp := range file.Imports {
		name := call.Name
		if call.Qualifier == "" {
			if imp.Name == "" || imp.Alias != call.Name {
				continue
			}
			name = imp.Name
		} else if imp.Name != "" || imp.Alias != call.Qualifier {
			continue
		}
		return uniqueSymbol(r.importedSymbols(file, imp.Path, name))
	}
	return Symbol{}, false
}

func (r *resolver) importedSymbols(file File, spec, name string) []Symbol {
	var matches []Symbol
	for _, targetFile := range r.importFiles(file, spec) {
		language := r.files[targetFile].Language
		if language != file.Language && (!jsFamily(language) || !jsFamily(file.Language)) {
			continue
		}
		for _, symbol := range topLevelNamed(r.symbols[targetFile], name) {
			if (jsFamily(file.Language) || file.Language == "go") && !symbol.Exported {
				continue
			}
			matches = append(matches, symbol)
		}
	}
	return matches
}

func topLevelNamed(symbols []Symbol, name string) []Symbol {
	var matches []Symbol
	for _, candidate := range symbols {
		if candidate.Name == name && topLevel(candidate) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func (r *resolver) lexical(file File, caller Symbol, name string) []Symbol {
	scope := caller.Qualified
	if scope == "" {
		scope = caller.Name
	}
	// A nested function is visible within its enclosing lexical function only.
	for scope != "" {
		qualified := scope + "::" + name
		var matches []Symbol
		for _, candidate := range file.Symbols {
			if candidate.Qualified == qualified && candidate.Kind == "function" {
				matches = append(matches, candidate)
			}
		}
		if len(matches) > 0 {
			return matches
		}
		at := strings.LastIndex(scope, "::")
		if at < 0 {
			break
		}
		scope = scope[:at]
	}
	matches := topLevelNamed(file.Symbols, name)
	if file.Language == "go" {
		matches = append(matches, r.goPackageSymbols(file, name)...)
	}
	return matches
}

func (r *resolver) goPackageSymbols(file File, name string) []Symbol {
	var matches []Symbol
	for _, other := range r.files {
		if other.Path == file.Path || other.Language != "go" || other.Package != file.Package || path.Dir(other.Path) != path.Dir(file.Path) {
			continue
		}
		matches = append(matches, topLevelNamed(other.Symbols, name)...)
	}
	return matches
}

func uniqueSymbol(symbols []Symbol) (Symbol, bool) {
	if len(symbols) != 1 {
		return Symbol{}, false
	}
	return symbols[0], true
}

func topLevel(symbol Symbol) bool {
	return symbol.Kind == "function" && (symbol.Qualified == "" || symbol.Qualified == symbol.Name)
}

func jsFamily(language string) bool {
	return language == "javascript" || language == "typescript" || language == "tsx"
}

func (r *resolver) importFiles(file File, spec string) []string {
	switch file.Language {
	case "go":
		return r.goImportFiles(spec)
	case "javascript", "typescript", "tsx":
		return r.jsImportFiles(file.Path, spec)
	case "python":
		return r.pythonImportFiles(file.Path, spec)
	}
	return nil
}

func (r *resolver) goImportFiles(spec string) []string {
	if r.module == "" || (spec != r.module && !strings.HasPrefix(spec, r.module+"/")) {
		return nil
	}
	dir := strings.TrimPrefix(strings.TrimPrefix(spec, r.module), "/")
	if dir == "" {
		dir = "."
	}
	var out []string
	for p, f := range r.files {
		if f.Language == "go" && path.Dir(p) == dir && !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func (r *resolver) jsImportFiles(from, spec string) []string {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return nil
	}
	base := path.Clean(path.Join(path.Dir(from), spec))
	if !inside(base) {
		return nil
	}
	// Do not guess between sibling .js/.ts implementations or tsconfig aliases.
	candidates := []string{base}
	if path.Ext(base) == "" {
		for _, ext := range []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"} {
			candidates = append(candidates, base+ext, path.Join(base, "index"+ext))
		}
	} else if strings.HasSuffix(base, ".js") {
		candidates = append(candidates, strings.TrimSuffix(base, ".js")+".ts", strings.TrimSuffix(base, ".js")+".tsx")
	}
	return r.oneFile(candidates)
}

func (r *resolver) pythonImportFiles(from, spec string) []string {
	base := strings.ReplaceAll(spec, ".", "/")
	if strings.HasPrefix(spec, ".") {
		dots := len(spec) - len(strings.TrimLeft(spec, "."))
		base = path.Dir(from)
		for i := 1; i < dots; i++ {
			base = path.Join(base, "..")
		}
		base = path.Join(base, strings.ReplaceAll(spec[dots:], ".", "/"))
	}
	if !inside(base) {
		return nil
	}
	return r.oneFile([]string{base + ".py", path.Join(base, "__init__.py")})
}

func inside(p string) bool { return !path.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../") }

func (r *resolver) oneFile(candidates []string) []string {
	var found []string
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if _, ok := r.files[candidate]; ok && !seen[candidate] {
			found = append(found, candidate)
			seen[candidate] = true
		}
	}
	if len(found) != 1 {
		return nil
	}
	return found
}
