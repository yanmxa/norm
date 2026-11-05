// Package codeindex builds cached multilingual syntax indexes and conservative call graphs.
package codeindex

import "path/filepath"

func language(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".py", ".pyi":
		return "python"
	}
	return ""
}
func Supported(path string) bool { return language(path) != "" }
