package codeindex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func put(t *testing.T, root, path, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func edgeNames(index *Index) []string {
	names := map[string]string{}
	for _, file := range index.Files {
		for _, s := range file.Symbols {
			names[s.ID] = s.File + ":" + s.Qualified
		}
	}
	var out []string
	for _, e := range index.Edges {
		out = append(out, names[e.Source]+" -> "+names[e.Target])
	}
	return out
}

func TestBuildLanguagesAndCache(t *testing.T) {
	root := t.TempDir()
	for p, src := range map[string]string{
		"go.mod":        "module example.org/app\n\ngo 1.26\n",
		"main.go":       "package main\nimport \"example.org/app/lib\"\nfunc main() { lib.Run() }\n",
		"lib/run.go":    "package lib\nfunc Run() {}\n",
		"web/util.ts":   "export function run() { return 1; }\n",
		"web/main.ts":   "import { run as execute } from './util';\nexport function main() { return execute(); }\n",
		"tools/util.py": "def run():\n    return 1\n",
		"tools/main.py": "from .util import run as execute\ndef main():\n    return execute()\n",
	} {
		put(t, root, p, src)
	}
	first, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Edges) != 3 {
		t.Fatalf("edges = %v, unresolved %d; want 3", edgeNames(first), first.Unresolved)
	}
	second, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("cached index differs from fresh index")
	}
	put(t, root, "tools/util.py", "def renamed():\n    return 1\n")
	changed, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Edges) != 2 {
		t.Fatalf("stale edge after rename: %v", edgeNames(changed))
	}
	if err := os.Remove(filepath.Join(root, "web/util.ts")); err != nil {
		t.Fatal(err)
	}
	deleted, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Edges) != 1 {
		t.Fatalf("stale edge after delete: %v", edgeNames(deleted))
	}
}

func TestDoNotGuessTargets(t *testing.T) {
	root := t.TempDir()
	for p, src := range map[string]string{
		"a.py":             "def run():\n    return 1\n",
		"b.py":             "def main():\n    return run()\n",
		"web/a.ts":         "export function run() { return 1; }\n",
		"web/b.ts":         "export function main(run: () => number) { return run(); }\n",
		"web/c.ts":         "import { run } from './ambiguous';\nexport function main() { return run(); }\n",
		"web/ambiguous.js": "export function run() { return 1; }\n",
		"web/ambiguous.ts": "export function run() { return 2; }\n",
	} {
		put(t, root, p, src)
	}
	index, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Edges) != 0 {
		t.Fatalf("guessed edges: %v", edgeNames(index))
	}
	if index.Unresolved < 2 {
		t.Fatalf("unresolved = %d", index.Unresolved)
	}
}

func TestBuildIgnoreAndParseFailure(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	put(t, root, ".gitignore", "ignored.py\n")
	put(t, root, "ignored.py", "not valid python (\n")
	put(t, root, "node_modules/bad.js", "not valid js {{{")
	put(t, root, "ok.py", "def run():\n    return 1\n")
	index, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Files) != 1 {
		t.Fatalf("files = %v", index.Files)
	}
	put(t, root, "ok.py", "def broken(\n")
	if _, err := Build(context.Background(), root); err == nil || !strings.Contains(err.Error(), "ok.py") {
		t.Fatalf("want parse failure, got %v", err)
	}
}

func TestImportVisibilityAndLexicalScope(t *testing.T) {
	root := t.TempDir()
	put(t, root, "lib.ts", "function hidden() { return 1; }\nexport function run() { return 2; }\n")
	put(t, root, "main.ts", "import { hidden } from './lib';\nexport function main() { return hidden(); }\nfunction help() { return 1; }\nexport function outer() { function help() { return 2; } return help(); }\n")
	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	names := edgeNames(idx)
	if len(names) != 1 || names[0] != "main.ts:outer -> main.ts:outer::help" {
		t.Fatalf("wrong visibility/scope resolution: %v", names)
	}
}

func TestIndexRejectsSymlinkCache(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	put(t, root, "main.py", "def run():\n    return 1\n")
	if err := os.Symlink(outside, filepath.Join(root, ".norm")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Build(context.Background(), root); err == nil {
		t.Fatal("accepted symlink cache")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("wrote through symlink cache")
	}
}

func TestBuildContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, t.TempDir()); err == nil {
		t.Fatal("cancelled build succeeded")
	}
}

func TestBuildModuleConfigInvalidatesEdges(t *testing.T) {
	root := t.TempDir()
	put(t, root, "go.mod", "module example.org/app\n")
	put(t, root, "main.go", "package main\nimport \"example.org/app/lib\"\nfunc main() { lib.Run() }\n")
	put(t, root, "lib/run.go", "package lib\nfunc Run() {}\n")
	index, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Edges) != 1 {
		t.Fatalf("edges: %v", edgeNames(index))
	}
	put(t, root, "go.mod", "module example.org/other\n")
	index, err = Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Edges) != 0 {
		t.Fatalf("stale module edge: %v", edgeNames(index))
	}
}
