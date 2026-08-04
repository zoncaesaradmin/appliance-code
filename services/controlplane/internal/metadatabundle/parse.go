package metadatabundle

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"gopkg.in/yaml.v3"
)

// LoadDirectory loads and validates a metadata-bundle directory tree.
func LoadDirectory(dir string) (*Bundle, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	manifestBytes, err := os.ReadFile(filepath.Join(abs, "bundle.yaml"))
	if err != nil {
		return nil, fmt.Errorf("metadatabundle: read bundle.yaml: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("metadatabundle: parse bundle.yaml: %w", err)
	}
	profileBytes, err := os.ReadFile(filepath.Join(abs, "profiles", "catalog.yaml"))
	if err != nil {
		return nil, fmt.Errorf("metadatabundle: read profiles/catalog.yaml: %w", err)
	}
	var profiles ProfileCatalog
	if err := yaml.Unmarshal(profileBytes, &profiles); err != nil {
		return nil, fmt.Errorf("metadatabundle: parse profiles/catalog.yaml: %w", err)
	}
	capBytes, err := os.ReadFile(filepath.Join(abs, "capabilities", "catalog.yaml"))
	if err != nil {
		return nil, fmt.Errorf("metadatabundle: read capabilities/catalog.yaml: %w", err)
	}
	var capabilities CapabilityCatalog
	if err := yaml.Unmarshal(capBytes, &capabilities); err != nil {
		return nil, fmt.Errorf("metadatabundle: parse capabilities/catalog.yaml: %w", err)
	}
	b := &Bundle{
		RootDir:      abs,
		Manifest:     manifest,
		Profiles:     profiles,
		Capabilities: capabilities,
	}
	if err := ValidateBundle(b); err != nil {
		return nil, err
	}
	return b, nil
}

// ExtractArchive extracts a .tar.zst metadata bundle into destParent and returns
// the top-level directory path.
func ExtractArchive(archivePath, destParent string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("metadatabundle: zstd reader: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)

	var topLevel string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("metadatabundle: tar: %w", err)
		}
		name := path.Clean("/" + hdr.Name)
		name = strings.TrimPrefix(name, "/")
		if name == "" || name == "." {
			continue
		}
		if strings.Contains(name, "..") {
			return "", fmt.Errorf("metadatabundle: path traversal rejected: %q", hdr.Name)
		}
		parts := strings.Split(name, "/")
		if topLevel == "" {
			topLevel = parts[0]
		} else if parts[0] != topLevel {
			return "", fmt.Errorf("metadatabundle: archive must contain exactly one top-level directory")
		}
		target := filepath.Join(destParent, filepath.FromSlash(name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			if err := out.Close(); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("metadatabundle: unsupported tar entry type %v for %q", hdr.Typeflag, hdr.Name)
		}
	}
	if topLevel == "" {
		return "", fmt.Errorf("metadatabundle: empty archive")
	}
	return filepath.Join(destParent, topLevel), nil
}

// PackDirectory creates a .tar.zst archive of dir (directory becomes top-level name).
func PackDirectory(dir, archivePath string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	base := filepath.Base(abs)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		name := base
		if rel != "." {
			name = pathJoin(base, filepath.ToSlash(rel))
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, f)
			f.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	out, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer out.Close()
	zw, err := zstd.NewWriter(out)
	if err != nil {
		return err
	}
	if _, err := zw.Write(buf.Bytes()); err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}

func pathJoin(a, b string) string {
	if b == "" || b == "." {
		return a
	}
	return a + "/" + b
}
