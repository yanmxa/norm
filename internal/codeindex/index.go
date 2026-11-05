package codeindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Edge is a statically resolved call, not a guess based on a repository-wide name.
type Edge struct {
	Source string
	Target string
	Kind   string
	Line   int
}

type Index struct {
	Files      []File
	Edges      []Edge
	Unresolved int
}

const cacheVersion = 2

type cachedFile struct {
	Hash string
	File File
}

type parseCache struct {
	Version int
	Files   map[string]cachedFile
}

// Build caches syntax by content, but resolves all relationships again so edits
// to definitions, imports, or module configuration cannot leave stale edges.
func Build(ctx context.Context, root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	paths, err := sourceFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(root, ".norm")
	if info, statErr := os.Lstat(cacheDir); statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return nil, fmt.Errorf("index directory must be a real directory: %s", cacheDir)
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return nil, statErr
	}
	cachePath := filepath.Join(cacheDir, "index.json")
	old := readCache(cachePath)
	next := parseCache{Version: cacheVersion, Files: make(map[string]cachedFile, len(paths))}
	index := &Index{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("index %s: %w", path, err)
		}
		sum := sha256.Sum256(src)
		hash := hex.EncodeToString(sum[:])
		entry, ok := old.Files[path]
		if !ok || entry.Hash != hash || entry.File.Path != path {
			file, err := Parse(ctx, path, src)
			if err != nil {
				return nil, fmt.Errorf("index %s: %w", path, err)
			}
			entry = cachedFile{Hash: hash, File: file}
		}
		next.Files[path] = entry
		index.Files = append(index.Files, entry.File)
	}
	resolve(root, index)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := writeCache(cachePath, next); err != nil {
		return nil, fmt.Errorf("save code index: %w", err)
	}
	return index, nil
}

func readCache(path string) parseCache {
	var cache parseCache
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &cache) != nil || cache.Version != cacheVersion {
		return parseCache{}
	}
	return cache
}

func writeCache(path string, cache parseCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "index-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func excluded(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) > 0 && (parts[0] == "dist" || parts[0] == "build" || parts[0] == "target") {
		return true
	}
	for _, part := range parts {
		switch part {
		case ".git", ".norm", ".codegraph", "node_modules", "vendor", ".venv", "venv", "__pycache__", "testdata":
			return true
		}
	}
	return false
}

func sourceFiles(ctx context.Context, root string) ([]string, error) {
	// Git supplies ignore semantics, including nested .gitignore files. A plain
	// directory remains usable, without following symlinks outside its root.
	probe := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	if data, err := probe.Output(); err == nil && sameRoot(root, strings.TrimSpace(string(data))) {
		cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("list source files: %w", err)
		}
		seen := map[string]bool{}
		var paths []string
		for _, path := range strings.Split(string(data), "\x00") {
			if !Supported(path) || excluded(path) || seen[path] {
				continue
			}
			info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() || hasSymlinkParent(root, path) {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return paths, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if excluded(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && d.Type().IsRegular() && Supported(rel) {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func sameRoot(a, b string) bool {
	aa, errA := filepath.EvalSymlinks(a)
	bb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && aa == bb
}

func hasSymlinkParent(root, path string) bool {
	for dir := filepath.Dir(filepath.FromSlash(path)); dir != "."; dir = filepath.Dir(dir) {
		info, err := os.Lstat(filepath.Join(root, dir))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
