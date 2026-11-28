package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const minimumInternalTokenLength = 32

func readInternalTokenFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("internal token file is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read internal token file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < minimumInternalTokenLength {
		return "", fmt.Errorf("internal token must contain at least %d bytes", minimumInternalTokenLength)
	}
	return token, nil
}
