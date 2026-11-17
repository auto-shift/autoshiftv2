// Package versions checks that every version AutoShift pins agrees with versions.yaml.
//
// Several versions are pinned in more than one place: Helm appears in two GitHub Actions steps,
// GitLab CI and the devcontainer Containerfile. Keeping those identical used to be a comment
// asking the next person to remember. This makes it a failing test instead.
package versions

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type location struct {
	File    string `yaml:"file"`
	Pattern string `yaml:"pattern"`
}

type pin struct {
	Name        string     `yaml:"name"`
	Value       string     `yaml:"value"`
	DerivedFrom string     `yaml:"derivedFrom"`
	ACMVendors  string     `yaml:"acmVendors"`
	Locations   []location `yaml:"locations"`
}

type manifest struct {
	Inputs map[string]string `yaml:"inputs"`
	Pins   []pin             `yaml:"pins"`
}

// repoRoot walks up until it finds versions.yaml.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "versions.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("could not find repo root (no versions.yaml in any parent)")
		}
		dir = parent
	}
}

func loadManifest(t *testing.T, root string) manifest {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "versions.yaml"))
	if err != nil {
		t.Fatalf("read versions.yaml: %v", err)
	}
	var m manifest
	if err := yaml.Unmarshal(body, &m); err != nil {
		t.Fatalf("parse versions.yaml: %v", err)
	}
	if len(m.Pins) == 0 {
		t.Fatal("versions.yaml declares no pins")
	}
	return m
}

// normalize makes v3.19.4 and 3.19.4 compare equal, so a table cell written without the prefix
// still counts as agreeing with the pin.
func normalize(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "v")
}

// TestVersionPins fails when any pinned version disagrees with versions.yaml.
func TestVersionPins(t *testing.T) {
	root := repoRoot(t)
	m := loadManifest(t, root)

	for _, p := range m.Pins {
		t.Run(p.Name, func(t *testing.T) {
			if p.Value == "" {
				t.Fatalf("pin %q has no value", p.Name)
			}
			if len(p.Locations) == 0 {
				t.Fatalf("pin %q lists no locations, so nothing enforces it", p.Name)
			}
			for _, loc := range p.Locations {
				body, err := os.ReadFile(filepath.Join(root, loc.File))
				if err != nil {
					t.Errorf("%s: %v", loc.File, err)
					continue
				}
				re, err := regexp.Compile(`(?m)` + loc.Pattern)
				if err != nil {
					t.Errorf("pin %q: bad pattern %q: %v", p.Name, loc.Pattern, err)
					continue
				}
				matches := re.FindAllStringSubmatch(string(body), -1)
				if len(matches) == 0 {
					t.Errorf(`%s: pattern %q matched nothing.
	       Either the pin moved to a different line or the file no longer sets it. Update the
	       pattern in versions.yaml, or drop this location if the version left that file.`,
						loc.File, loc.Pattern)
					continue
				}
				for _, mt := range matches {
					if normalize(mt[1]) != normalize(p.Value) {
						t.Errorf(`%s: %s is %q, want %q
	       versions.yaml is the source of truth. Update this file, or change the value in
	       versions.yaml if the pin genuinely moved.`,
							loc.File, p.Name, mt[1], p.Value)
					}
				}
			}
		})
	}
}

// TestInputsAreReferenced fails an input that no pin derives from, which would mean the input is
// recorded but nothing actually follows it.
func TestInputsAreReferenced(t *testing.T) {
	root := repoRoot(t)
	m := loadManifest(t, root)

	if len(m.Inputs) == 0 {
		t.Fatal("versions.yaml declares no inputs")
	}
	derived := map[string]bool{}
	for _, p := range m.Pins {
		derived[p.DerivedFrom] = true
	}
	for name := range m.Inputs {
		if !derived[name] {
			t.Errorf("input %q is declared but no pin derives from it", name)
		}
	}
	// Every derivedFrom must name an input, another pin, or one of the two literals.
	names := map[string]bool{"input": true, "none": true}
	for name := range m.Inputs {
		names[name] = true
	}
	for _, p := range m.Pins {
		names[p.Name] = true
	}
	for _, p := range m.Pins {
		if !names[p.DerivedFrom] {
			t.Errorf("pin %q derives from %q, which is neither an input nor another pin",
				p.Name, p.DerivedFrom)
		}
	}
}
