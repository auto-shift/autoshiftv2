package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot walks up from the package directory to find the repository root
// (identified by the presence of a `policies/` directory).
//
// This lives in an untagged file because both the untagged unit tests and the
// integration-tagged suite use it. Keeping it behind //go:build integration
// broke `go test ./...` with "undefined: repoRoot".
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "policies")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("could not find repo root (no policies/ directory in any parent)")
		}
		dir = parent
	}
}
