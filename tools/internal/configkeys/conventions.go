// Package configkeys enforces the naming contract between a policy and the
// top-level config key it owns.
//
// A policy that owns configuration reads a single top-level key from the
// cluster's rendered-config ConfigMap, named after the policy directory in
// lowerCamelCase. Keys that belong to the fleet rather than to one policy, and
// keys that deliberately use a shorter name, are recorded as exceptions so each
// deviation is visible in review.
package configkeys

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Conventions records the deviations from the directory-name rule.
type Conventions struct {
	// Shared keys carry fleet-wide facts read by several policies.
	Shared []string `yaml:"shared"`
	// Aliases map a config key to the policy directory that owns it.
	Aliases map[string]string `yaml:"aliases"`
}

// Report is the outcome of checking declared keys against the convention.
type Report struct {
	// OK keys resolve to a policy directory or a recorded exception.
	OK []string
	// Unmapped keys match no policy directory and no exception.
	Unmapped []string
	// StaleAliases name a policy directory that no longer exists.
	StaleAliases []string
}

// CamelCase converts a policy directory name to the config key it owns:
// workload-partitioning becomes workloadPartitioning.
func CamelCase(dir string) string {
	parts := strings.Split(dir, "-")
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// LoadConventions reads the exception file. A missing file is not an error: it
// means no exceptions are recorded.
func LoadConventions(path string) (*Conventions, error) {
	c := &Conventions{Aliases: map[string]string{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if c.Aliases == nil {
		c.Aliases = map[string]string{}
	}
	return c, nil
}

// PolicyDirs lists every policy directory under policies/<tier>/<name>.
func PolicyDirs(policiesRoot string) ([]string, error) {
	var out []string
	tiers, err := os.ReadDir(policiesRoot)
	if err != nil {
		return nil, err
	}
	for _, tier := range tiers {
		if !tier.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(policiesRoot, tier.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// ExtractDeclared returns every key declared inside a config: mapping in any
// _example*.yaml file under valuesRoot, mapped to the files declaring it.
func ExtractDeclared(valuesRoot string) (map[string][]string, error) {
	declared := map[string][]string{}
	err := filepath.Walk(valuesRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		name := filepath.Base(path)
		if !strings.HasPrefix(name, "_example") || filepath.Ext(name) != ".yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc map[string]interface{}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		walkForConfig(doc, name, declared)
		return nil
	})
	return declared, err
}

// walkForConfig recurses looking for config: mappings. The keys directly inside
// one are the declarations; it does not recurse into their values, so a nested
// key named "config" is treated as data rather than another declaration block.
func walkForConfig(obj map[string]interface{}, file string, out map[string][]string) {
	for k, v := range obj {
		child, isMap := v.(map[string]interface{})
		if !isMap {
			continue
		}
		if k == "config" {
			for ck := range child {
				if !contains(out[ck], file) {
					out[ck] = append(out[ck], file)
				}
			}
			continue
		}
		walkForConfig(child, file, out)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// BuildReport checks each declared key against the policy directories and the
// recorded exceptions.
func BuildReport(declared map[string][]string, dirs []string, conv *Conventions) Report {
	owned := map[string]bool{}
	dirSet := map[string]bool{}
	for _, d := range dirs {
		owned[CamelCase(d)] = true
		dirSet[d] = true
	}
	shared := map[string]bool{}
	for _, s := range conv.Shared {
		shared[s] = true
	}

	var rep Report
	for key := range declared {
		switch {
		case owned[key], shared[key]:
			rep.OK = append(rep.OK, key)
		default:
			if _, ok := conv.Aliases[key]; ok {
				rep.OK = append(rep.OK, key)
			} else {
				rep.Unmapped = append(rep.Unmapped, key)
			}
		}
	}
	for key, dir := range conv.Aliases {
		if !dirSet[dir] {
			rep.StaleAliases = append(rep.StaleAliases, fmt.Sprintf("%s -> %s", key, dir))
		}
	}
	sort.Strings(rep.OK)
	sort.Strings(rep.Unmapped)
	sort.Strings(rep.StaleAliases)
	return rep
}
