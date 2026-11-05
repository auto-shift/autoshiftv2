//go:build integration

package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Two repository non-negotiables that nothing enforced until now:
//
//   - every ConfigurationPolicy keeps an evaluationInterval with BOTH compliant and noncompliant.
//     The default is `watch`, so a missing block changes evaluation behaviour silently.
//   - every policy directory carries a README.md.
//
// Both are cheap to check statically from the PolicyGenerator config, and both are the kind of
// omission that reviewers catch inconsistently.

type pgConfig struct {
	PolicyDefaults struct {
		EvaluationInterval map[string]string `yaml:"evaluationInterval"`
	} `yaml:"policyDefaults"`
	Policies []struct {
		Name               string            `yaml:"name"`
		EvaluationInterval map[string]string `yaml:"evaluationInterval"`
	} `yaml:"policies"`
}

func hasBothIntervals(m map[string]string) bool {
	return m["compliant"] != "" && m["noncompliant"] != ""
}

// maxPolicyDirsWithoutReadme is a ratchet, not an allowlist. 28 directories predate the check.
// Lower it when you add a README; never raise it. A new policy without one pushes the count over
// the limit and fails.
const maxPolicyDirsWithoutReadme = 28

// checkPolicyStructure returns the policy directories with no README and, separately, every
// evaluationInterval violation under policiesDir.
func checkPolicyStructure(policiesDir string) (noReadme []string, intervals []string) {
	var msgs []string

	entries, err := os.ReadDir(policiesDir)
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot read %s: %v", policiesDir, err)}
	}
	for _, tier := range entries {
		if !tier.IsDir() {
			continue
		}
		tierDir := filepath.Join(policiesDir, tier.Name())
		policies, err := os.ReadDir(tierDir)
		if err != nil {
			continue
		}
		for _, pol := range policies {
			if !pol.IsDir() {
				continue
			}
			dir := filepath.Join(tierDir, pol.Name())
			id := tier.Name() + "/" + pol.Name()

			pgPath := filepath.Join(dir, "policy-generator-config.yaml")
			isPG := false
			if _, err := os.Stat(pgPath); err == nil {
				isPG = true
			} else if _, err := os.Stat(filepath.Join(dir, "Chart.yaml")); err != nil {
				continue // not a policy directory
			}

			if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
				noReadme = append(noReadme, id)
			}

			if !isPG {
				continue
			}
			body, err := os.ReadFile(pgPath)
			if err != nil {
				msgs = append(msgs, fmt.Sprintf("%s: cannot read policy-generator-config.yaml: %v", id, err))
				continue
			}
			var cfg pgConfig
			if err := yaml.Unmarshal(body, &cfg); err != nil {
				msgs = append(msgs, fmt.Sprintf("%s: policy-generator-config.yaml does not parse: %v", id, err))
				continue
			}
			defaultOK := hasBothIntervals(cfg.PolicyDefaults.EvaluationInterval)
			for _, p := range cfg.Policies {
				if defaultOK || hasBothIntervals(p.EvaluationInterval) {
					continue
				}
				msgs = append(msgs, fmt.Sprintf(
					"%s: policy %q has no evaluationInterval with both compliant and noncompliant. "+
						"Set it in policyDefaults as ${EVAL_COMPLIANT} and ${EVAL_NONCOMPLIANT}, or on "+
						"this entry. Without it the interval defaults to `watch` silently.", id, p.Name))
			}
		}
	}
	return noReadme, msgs
}

// TestPolicyStructure enforces the two non-negotiables above across the real policy tree.
func TestPolicyStructure(t *testing.T) {
	root := repoRoot(t)
	noReadme, intervals := checkPolicyStructure(filepath.Join(root, "policies"))

	for _, m := range intervals {
		t.Errorf(`%s`, m)
	}

	if len(noReadme) > maxPolicyDirsWithoutReadme {
		t.Errorf(`%d policy directories have no README.md, up from the allowed %d.

	       A new policy directory needs a README.md describing what it does and the labels and
	       config it reads. Add one, or if you removed a README, restore it.
	       Directories without one: %v`, len(noReadme), maxPolicyDirsWithoutReadme, noReadme)
	}

	t.Logf("policy structure: %d evaluationInterval violations, %d directories without a README (limit %d)",
		len(intervals), len(noReadme), maxPolicyDirsWithoutReadme)
}

// pgManifests maps each ConfigurationPolicy name a PolicyGenerator config produces back to the
// manifest file it came from. PolicyGenerator names multiple manifests in one policies[] entry
// policy-x, policy-x2, policy-x3 in manifest order, which is impossible to map by eye and has
// cost real debugging time.
func pgManifests(policiesDir, chart string) map[string]string {
	out := map[string]string{}
	body, err := os.ReadFile(filepath.Join(policiesDir, chart, "policy-generator-config.yaml"))
	if err != nil {
		return out
	}
	var cfg struct {
		PolicyDefaults struct {
			ConsolidateManifests *bool `yaml:"consolidateManifests"`
		} `yaml:"policyDefaults"`
		Policies []struct {
			Name                 string `yaml:"name"`
			ConsolidateManifests *bool  `yaml:"consolidateManifests"`
			Manifests            []struct {
				Path string `yaml:"path"`
				// A manifest may override the generated policy name. When it does, that name
				// is what reaches the cluster, so the index-derived name never appears.
				Name string `yaml:"name"`
			} `yaml:"manifests"`
		} `yaml:"policies"`
	}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return out
	}
	for _, p := range cfg.Policies {
		// consolidateManifests only merges PLAIN object manifests. A manifest that is itself an
		// object-templates fragment always becomes its own ConfigurationPolicy, numbered
		// policy-x, policy-x2, policy-x3 in manifest order, whatever the flag says. Most
		// manifests here are fragments, so classify rather than trust the flag.
		allFragments := len(p.Manifests) > 0
		for _, m := range p.Manifests {
			body, err := os.ReadFile(filepath.Join(policiesDir, chart, m.Path))
			if err != nil || !objectTemplatesFragment(string(body)) {
				allFragments = false
				break
			}
		}
		if !allFragments {
			paths := make([]string, 0, len(p.Manifests))
			for _, m := range p.Manifests {
				paths = append(paths, m.Path)
			}
			out[p.Name] = strings.Join(paths, ", ")
			continue
		}
		for i, m := range p.Manifests {
			name := p.Name
			if i > 0 {
				name = fmt.Sprintf("%s%d", p.Name, i+1)
			}
			if m.Name != "" {
				name = m.Name
			}
			out[name] = m.Path
		}
	}
	return out
}

// objectTemplatesFragment reports whether a manifest is an object-templates fragment rather than a
// plain Kubernetes object, which decides whether PolicyGenerator gives it its own
// ConfigurationPolicy.
func objectTemplatesFragment(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "object-templates:") || strings.HasPrefix(line, "object-templates-raw:") {
			return true
		}
	}
	return false
}
