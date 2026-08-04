// Command pack-tar-zst writes a directory tree to a .tar.zst archive.
// Packaging scripts use this when the host has neither the zstd CLI nor
// the Python zstandard package. It is a tiny, self-contained module with
// a vendored compressor so air-gapped builders need no module download and
// no Go toolchain auto-upgrade.
package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

func main() {
	srcDir := flag.String("src", "", "directory to pack (top-level name is preserved in the archive)")
	outPath := flag.String("out", "", "output .tar.zst path")
	flag.Parse()
	if strings.TrimSpace(*srcDir) == "" || strings.TrimSpace(*outPath) == "" {
		fmt.Fprintln(os.Stderr, "usage: pack-tar-zst -src DIR -out FILE.tar.zst")
		os.Exit(2)
	}
	srcDirAbs, err := filepath.Abs(*srcDir)
	if err != nil {
		fatal(err)
	}
	info, err := os.Stat(srcDirAbs)
	if err != nil {
		fatal(err)
	}
	if !info.IsDir() {
		fatal(fmt.Errorf("src is not a directory: %s", srcDirAbs))
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fatal(err)
	}
	out, err := os.Create(*outPath)
	if err != nil {
		fatal(err)
	}
	defer out.Close()
	zw, err := zstd.NewWriter(out)
	if err != nil {
		fatal(err)
	}
	tw := tar.NewWriter(zw)

	base := filepath.Base(srcDirAbs)
	err = filepath.WalkDir(srcDirAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDirAbs, path)
		if err != nil {
			return err
		}
		name := base
		if rel != "." {
			name = filepath.ToSlash(filepath.Join(base, rel))
		} else {
			name = base + "/"
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		if d.IsDir() {
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
			return tw.WriteHeader(hdr)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unsupported file type for %s", path)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		f.Close()
		return copyErr
	})
	if err != nil {
		_ = tw.Close()
		_ = zw.Close()
		fatal(err)
	}
	if err := tw.Close(); err != nil {
		_ = zw.Close()
		fatal(err)
	}
	if err := zw.Close(); err != nil {
		fatal(err)
	}
	if err := out.Close(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "pack-tar-zst:", err)
	os.Exit(1)
}
