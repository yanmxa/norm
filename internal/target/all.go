package target

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanmxa/metron/internal/codeindex"
)

// ResolveAll builds a Target covering every supported source file in the repository rather
// than the files one change touched.
//
// This is a different question from the diff-scoped one and it answers fewer
// things honestly. There is no base revision, so nothing can say whether a
// function got worse or whether an edge is new — those readings report as
// unmeasured rather than guessing. What survives is everything about the code
// as it stands now: how hard it is to read, what nothing reaches, and what the
// tests fail to pin.
func ResolveAll(ctx context.Context, dir string) (*Target, error) {
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	var included map[string]bool
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		root = dir // usable outside a repository; only Δ needs git
	} else {
		listed, err := git(ctx, strings.TrimSpace(root), "ls-files", "--cached", "--others", "--exclude-standard", "-z")
		if err != nil {
			return nil, err
		}
		included = map[string]bool{}
		for _, p := range strings.Split(listed, "\x00") {
			included[p] = true
		}
	}
	root, err = filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, err
	}

	var files []ChangedFile
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if path != root && excludedPath(filepath.ToSlash(rel)+"/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !codeindex.Supported(path) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if included != nil && !included[filepath.ToSlash(rel)] {
			return nil
		}
		n, cerr := countFileLines(path)
		if cerr != nil {
			return cerr
		}
		if n == 0 {
			return nil
		}
		files = append(files, ChangedFile{
			Path:   filepath.ToSlash(rel),
			Status: Modified,
			Ranges: []LineRange{{Start: 1, End: n}},
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no supported source files under %s", root)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	return &Target{
		Root: root, BaseRef: "", BaseSHA: "",
		HeadDesc: "whole repository", Files: files, WholeRepo: true,
	}, nil
}

func countFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		n++
	}
	return n, sc.Err()
}

func skipDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "testdata", ".codegraph", ".metron", "venv", "__pycache__":
		return true
	}
	return strings.HasPrefix(name, ".")
}
