package redact

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Extensions are the file extensions the redactor opens, in lower case.
var Extensions = []string{".png", ".jpg", ".jpeg"}

// IsPicture reports whether name has one of the [Extensions].
func IsPicture(name string) bool {
	return slices.Contains(Extensions, strings.ToLower(filepath.Ext(name)))
}

// Load opens the PNG or JPEG file at path.
func Load(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if format != "png" && format != "jpeg" {
		return nil, fmt.Errorf("%s: format %s is not supported", filepath.Base(path), format)
	}
	return New(path, format, img), nil
}

// Encode writes img to w in format, "png" or "jpeg". The encoders write no
// metadata, so a saved JPEG also loses its EXIF block, location included.
func Encode(w io.Writer, img image.Image, format string) error {
	switch format {
	case "png":
		return png.Encode(w, img)
	case "jpeg":
		return jpeg.Encode(w, img, &jpeg.Options{Quality: 95})
	default:
		return fmt.Errorf("format %s is not supported", format)
	}
}

// WriteFile encodes img and replaces the file at path with it. The picture is
// written next to the file first and renamed over it, so a failure halfway
// leaves the old file intact. The file keeps its permissions.
func WriteFile(path string, img image.Image, format string) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename succeeded
	if err := Encode(tmp, img, format); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
