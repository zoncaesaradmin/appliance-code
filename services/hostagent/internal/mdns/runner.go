package mdns

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner executes host commands.
type Runner interface {
	LookPath(file string) (string, error)
	CombinedOutput(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner shells out on the real host.
type ExecRunner struct {
	Timeout time.Duration
}

func (r ExecRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (r ExecRunner) CombinedOutput(ctx context.Context, name string, args ...string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		if out == "" {
			return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
	}
	return out, nil
}

// FileIO abstracts state filesystem operations for tests.
type FileIO interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	ReadFile(path string) ([]byte, error)
	Remove(path string) error
}

// OSFileIO uses the real filesystem.
type OSFileIO struct{}

func (OSFileIO) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFileIO) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (OSFileIO) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (OSFileIO) Remove(path string) error             { return os.Remove(path) }
