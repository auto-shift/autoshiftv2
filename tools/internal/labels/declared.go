package labels

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Declaration records one place where a label key is declared in a values file.
type Declaration struct {
	Key         string // label key (no `autoshift.io/` prefix — values files use the bare key)
	File        string // repo-relative path to the values file
	Path        string // dot-path to the labels block, e.g. `hubClusterSets.hub.labels`
	Value       string // the declared value, as a string (`"true"`, `stable`, etc.). Empty if non-scalar.
	FromExample bool   // true if File is an `_example*.yaml` catalog file
}

// Declared is the aggregate view of one label key across all its declarations.
type Declared struct {
	Key          string
	Declarations []Declaration
}

// InExamples returns true if this key is declared in at least one
// `_example*.yaml` file. AutoShift treats example files as the authoritative
// label catalog — a key is only considered "properly declared" if it's in
// an example.
func (d Declared) InExamples() bool {
	for _, decl := range d.Declarations {
		if decl.FromExample {
			return true
		}
	}
	return false
}

// Files returns the sorted unique set of files where this label is declared.
func (d Declared) Files() []string {
	set := map[string]struct{}{}
	for _, decl := range d.Declarations {
		set[decl.File] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// ExtractDeclaredFromFile parses a values file and returns every label key
// declared under any `labels:` block (at any nesting depth). Only active
// (uncommented) YAML keys are captured — if a label should be in the catalog,
// it must be a real YAML key, not a comment.
func ExtractDeclaredFromFile(path string) ([]Declaration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []Declaration
	walkForLabels(&root, "", path, &out)
	return out, nil
}

// walkForLabels performs a recursive descent over a YAML node tree looking for
// mapping keys named `labels` whose value is itself a mapping. Each entry in
// such a labels mapping becomes a Declaration.
func walkForLabels(node *yaml.Node, path, file string, out *[]Declaration) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, c := range node.Content {
			walkForLabels(c, path, file, out)
		}
	case yaml.MappingNode:
		// MappingNode content is [key, value, key, value, ...].
		for i := 0; i+1 < len(node.Content); i += 2 {
			kNode := node.Content[i]
			vNode := node.Content[i+1]
			keyPath := joinPath(path, kNode.Value)

			if kNode.Value == "labels" && vNode.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(vNode.Content); j += 2 {
					lk := vNode.Content[j]
					lv := vNode.Content[j+1]
					val := ""
					if lv.Kind == yaml.ScalarNode {
						val = lv.Value
					}
					*out = append(*out, Declaration{
						Key:   lk.Value,
						File:  file,
						Path:  keyPath,
						Value: val,
					})
				}
				// Don't recurse into the labels block itself — label values
				// should not be treated as further labels.
				continue
			}
			walkForLabels(vNode, keyPath, file, out)
		}
	case yaml.SequenceNode:
		for i, c := range node.Content {
			walkForLabels(c, fmt.Sprintf("%s[%d]", path, i), file, out)
		}
	}
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

// commentedLabelLinePattern matches a commented-out label entry inside a
// labels block. Example files use this form to document optional labels:
//
//	      # gitops-namespace: 'openshift-gitops'  # Override ArgoCD namespace per-cluster
//
// Capture group 1 is the label key. We accept a variable leading indent so
// the same pattern works at whatever nesting depth the labels block sits at.

// IsExampleFile returns true if the given file name is an `_example*.yaml`
// catalog file. AutoShift's convention treats example files as the
// authoritative set of available labels; the other profiles (hub.yaml,
// managed.yaml, sbx.yaml, etc.) are curated recommended subsets.
func IsExampleFile(fileName string) bool {
	return strings.HasPrefix(fileName, "_example")
}

// ExtractDeclaredFromTree walks valuesDir and aggregates declarations from
// values files into a per-key Declared map.
//
// By default only `_example*.yaml` files are considered — the other profiles
// are just recommended subsets and don't define the full label catalog. Pass
// `includeProfiles=true` to also collect declarations from non-example files
// (useful for the secondary "profile drift" check, or for inspection).
func ExtractDeclaredFromTree(valuesDir string, includeProfiles bool) (map[string]*Declared, error) {
	all := map[string]*Declared{}

	err := filepath.WalkDir(valuesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		if !IsExampleFile(name) && !includeProfiles {
			return nil
		}
		decls, err := ExtractDeclaredFromFile(path)
		if err != nil {
			return err
		}
		fromExample := IsExampleFile(name)
		for _, decl := range decls {
			decl.FromExample = fromExample
			d, ok := all[decl.Key]
			if !ok {
				d = &Declared{Key: decl.Key}
				all[decl.Key] = d
			}
			d.Declarations = append(d.Declarations, decl)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// SortedDeclared returns Declared values sorted by key.
func SortedDeclared(m map[string]*Declared) []*Declared {
	out := make([]*Declared, 0, len(m))
	for _, d := range m {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
