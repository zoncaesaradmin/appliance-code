package httpapi

import (
	"fmt"
	"path"
	"strings"
)

// blobRelativePath cleans a public library/object path. Empty paths are allowed
// when required is false (directory listing of the prefix root).
func blobRelativePath(raw string, required bool) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "." || trimmed == "/" {
		if required {
			return "", fmt.Errorf("file path is required")
		}
		return "", nil
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+trimmed), "/")
	if cleaned == "" || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid file path")
	}
	return cleaned, nil
}
