package labels

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp creates a temporary file with the given name and body, returning
// its path. The file is cleaned up automatically when the test ends.
func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writeTemp %s: %v", name, err)
	}
	return path
}
