package labels

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// allowlistFile is the YAML schema for .github/label-lint-allowlist.yaml.
type allowlistFile struct {
	// Keys here may be consumed by policy templates without being declared
	// in any values file.
	MissingOK []string `yaml:"missing_ok"`
	// Keys here may be declared in values files without being consumed by
	// any policy template.
	OrphanedOK []string `yaml:"orphaned_ok"`
}

// LoadAllowlist reads a YAML allowlist file. Returns an empty (non-nil)
// Allowlist if path is empty or the file does not exist.
func LoadAllowlist(path string) (*Allowlist, error) {
	out := &Allowlist{
		MissingOK:  map[string]bool{},
		OrphanedOK: map[string]bool{},
	}
	if path == "" {
		return out, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read allowlist %s: %w", path, err)
	}
	var f allowlistFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse allowlist %s: %w", path, err)
	}
	for _, k := range f.MissingOK {
		out.MissingOK[k] = true
	}
	for _, k := range f.OrphanedOK {
		out.OrphanedOK[k] = true
	}
	return out, nil
}
