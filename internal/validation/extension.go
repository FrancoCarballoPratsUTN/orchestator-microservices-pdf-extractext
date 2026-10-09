package validation

import (
	"fmt"
	"path/filepath"
	"strings"
)

// checkExtension rejects a file name whose final path segment does not end in
// .pdf (case-insensitive). The endpoint receives raw bytes, so the name is
// optional metadata: an empty name skips the check.
func checkExtension(filename string) error {
	if filename == "" {
		return nil
	}
	if strings.EqualFold(filepath.Ext(lastPathSegment(filename)), ".pdf") {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnsupportedExtension, filename)
}

// lastPathSegment returns the portion after the final separator, normalizing
// both slash styles so a Windows path is handled like a POSIX one.
func lastPathSegment(filename string) string {
	if i := strings.LastIndexAny(filename, `/\`); i >= 0 {
		return filename[i+1:]
	}
	return filename
}
