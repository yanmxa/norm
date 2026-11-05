package codeindex

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func Parse(ctx context.Context, path string, src []byte) (File, error) {
	f := File{Path: filepath.ToSlash(filepath.Clean(path)), Language: language(path)}
	if err := ctx.Err(); err != nil {
		return f, err
	}
	if f.Language == "" {
		return f, fmt.Errorf("unsupported language: %s", path)
	}
	var err error
	if f.Language == "go" {
		err = parseGo(ctx, &f, src)
	} else {
		err = parseTree(ctx, &f, src)
	}
	if err != nil {
		return File{}, err
	}
	counts := map[string]int{}
	for i := range f.Symbols {
		s := &f.Symbols[i]
		s.File, s.Language = f.Path, f.Language
		key := f.Path + "::" + s.Qualified
		counts[key]++
		s.ID = key
		if counts[key] > 1 {
			s.ID = fmt.Sprintf("%s#%d", key, counts[key])
		}
	}
	sort.Strings(f.Identifiers)
	f.Identifiers = unique(f.Identifiers)
	return f, ctx.Err()
}
func unique(xs []string) []string {
	out := xs[:0]
	for _, x := range xs {
		if len(out) == 0 || out[len(out)-1] != x {
			out = append(out, x)
		}
	}
	return out
}
func fingerprint(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func qualify(scope, name string) string {
	if scope == "" {
		return name
	}
	return scope + "::" + name
}
func compact(s string) string { return strings.Join(strings.Fields(s), " ") }
