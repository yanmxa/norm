package target

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAllNativeLanguagesAndExclusions(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a.py", "b.ts", "c.tsx", "d.js", "e.go", "internal/target/source.go", "node_modules/skip.ts", ".venv/skip.py", "vendor/skip.go", "target/skip.go", "build/skip.ts", "dist/skip.js"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("source\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "a.py"), filepath.Join(root, "link.py")); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveAll(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 6 {
		t.Fatalf("files=%+v", got.Files)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveAll(ctx, root); err == nil {
		t.Fatal("cancelled traversal succeeded")
	}
	if _, err := ResolveAll(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing root succeeded")
	}
}
