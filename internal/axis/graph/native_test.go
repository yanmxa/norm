package graph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yanmxa/metron/internal/axis"
	"github.com/yanmxa/metron/internal/codeindex"
	"github.com/yanmxa/metron/internal/target"
)

func TestNativeChangedCalls(t *testing.T) {
	for _, tc := range []struct{ ext, base, noop, added string }{
		{"py", "def old():\n    return 1\ndef fresh():\n    return 2\ndef run():\n    return old()\n", "def old():\n    return 1\ndef fresh():\n    return 2\ndef run():\n    return old() # comment\n", "def old():\n    return 1\ndef fresh():\n    return 2\ndef run():\n    return old() + fresh()\n"},
		{"ts", "function old() { return 1; }\nfunction fresh() { return 2; }\nfunction run() { return old(); }\n", "function old() { return 1; }\nfunction fresh() { return 2; }\nfunction run() { return old(); /* comment */ }\n", "function old() { return 1; }\nfunction fresh() { return 2; }\nfunction run() { return old() + fresh(); }\n"},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			root := t.TempDir()
			gitRun := func(args ...string) {
				t.Helper()
				c := exec.Command("git", args...)
				c.Dir = root
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
			}
			path := "main." + tc.ext
			write := func(src string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, path), []byte(src), 0600); err != nil {
					t.Fatal(err)
				}
			}
			gitRun("init", "-q")
			gitRun("config", "user.email", "test@example.com")
			gitRun("config", "user.name", "Test")
			write(tc.base)
			gitRun("add", ".")
			gitRun("commit", "-qm", "base")
			a := New(DefaultConfig())
			ctx := context.Background()
			for _, step := range []struct {
				src   string
				fresh bool
			}{{tc.noop, false}, {tc.added, true}} {
				write(step.src)
				target, err := target.Resolve(ctx, root, "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				idx, err := codeindex.Build(ctx, root)
				if err != nil {
					t.Fatal(err)
				}
				g := FromIndex(idx)
				changed, nc, missing, err := a.changedNodes(ctx, target, g)
				if err != nil || missing != 0 {
					t.Fatalf("changed: missing=%d err=%v", missing, err)
				}
				if len(changed) != 1 || changed[0].Name != "run" {
					t.Fatalf("changed=%+v", changed)
				}
				if got := nc.Gained(changed[0].Key(), "fresh"); got != step.fresh {
					t.Fatalf("fresh=%v want %v", got, step.fresh)
				}
				hasFreshEdge := false
				for id := range g.Callees(changed[0].ID) {
					if g.Nodes[id].Name == "fresh" {
						hasFreshEdge = true
					}
				}
				if hasFreshEdge != step.fresh {
					t.Fatalf("resolved dependency edge=%v want %v", hasFreshEdge, step.fresh)
				}
				if nc.Gained(changed[0].Key(), "old") {
					t.Fatal("preexisting call counted as new")
				}
				r, err := a.Run(ctx, target, nil)
				if err != nil {
					t.Fatal(err)
				}
				if r.Diagnostics["graph.orphans"] != 0 {
					t.Fatal("non-Go orphan finding")
				}
				target.BaseSHA = "not-a-revision"
				r, err = a.Run(ctx, target, nil)
				if err != nil {
					t.Fatal(err)
				}
				if r.Measures[1].Status != axis.StatusUnmeasured {
					t.Fatal("baseline failure was measured")
				}
			}
			// Renaming must read the original baseline path, not invent all calls.
			write(tc.noop)
			oldPath := path
			path = "renamed." + tc.ext
			gitRun("mv", oldPath, path)
			tg := &target.Target{Root: root, BaseSHA: "HEAD", Files: []target.ChangedFile{{Path: path, OldPath: oldPath, Status: target.Renamed, Ranges: []target.LineRange{{Start: 1, End: 100}}}}}
			idx, err := codeindex.Build(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			changed, nc, missing, err := a.changedNodes(ctx, tg, FromIndex(idx))
			if err != nil || missing != 0 {
				t.Fatalf("rename missing=%d err=%v", missing, err)
			}
			for _, n := range changed {
				if nc.Introduced(n.Key()) {
					t.Fatal("rename introduced calls")
				}
			}
			// Added files intentionally have no baseline; their calls really are new.
			tg.Files[0].Status = target.Added
			tg.BaseSHA = "invalid-but-not-needed"
			changed, nc, _, err = a.changedNodes(ctx, tg, FromIndex(idx))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, n := range changed {
				if n.Name == "run" {
					found = nc.Gained(n.Key(), "old")
				}
			}
			if !found {
				t.Fatal("added file call not identified")
			}
		})
	}
}
