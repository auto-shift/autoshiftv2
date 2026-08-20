//go:build integration

package configkeys

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot walks up from the package directory to the repository root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "policies")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate repository root")
	return ""
}

// TestConfigKeyConventions fails when a config key declared in an example
// values file matches neither a policy directory nor a recorded exception.
// That is the signal that a new policy added config without documenting it, or
// documented it under a name nothing owns.
func TestConfigKeyConventions(t *testing.T) {
	root := repoRoot(t)

	conv, err := LoadConventions(filepath.Join(root, ".github", "config-key-conventions.yaml"))
	if err != nil {
		t.Fatalf("loading conventions: %v", err)
	}
	dirs, err := PolicyDirs(filepath.Join(root, "policies"))
	if err != nil {
		t.Fatalf("listing policy directories: %v", err)
	}
	declared, err := ExtractDeclared(filepath.Join(root, "autoshift", "values"))
	if err != nil {
		t.Fatalf("extracting declared config keys: %v", err)
	}
	if len(declared) == 0 {
		t.Fatal("no config keys found in any _example*.yaml; the extractor is broken")
	}

	rep := BuildReport(declared, dirs, conv)
	t.Logf("%d config keys checked, %d resolve to a policy or recorded exception", len(declared), len(rep.OK))

	for _, key := range rep.Unmapped {
		t.Errorf("config key %q (declared in %v) matches no policy directory and is not recorded in "+
			".github/config-key-conventions.yaml.\n"+
			"  Either rename it to the lowerCamelCase form of its policy directory, or add it to "+
			"shared: (fleet-wide) or aliases: (short name for a policy).", key, declared[key])
	}
	for _, stale := range rep.StaleAliases {
		t.Errorf("alias %s names a policy directory that does not exist; remove or correct the entry "+
			"in .github/config-key-conventions.yaml", stale)
	}
}
