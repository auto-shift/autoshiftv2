//go:build integration

package resolver

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A ConfigurationPolicy with nothing to apply reports Compliant. That is the most
// dangerous failure mode in this repository: a policy whose required input is missing
// renders no object-templates, the governance dashboard goes green, and nothing is
// enforced. TestPipeline_EndToEnd cannot see it — it checks that the resolved document
// is valid YAML, and a policy with an empty template list is perfectly valid.
//
// TestObjectTemplatesRaw_ParsesAsYAML walks the same structure for parseability and skips
// empty blocks. This is the counterpart: it cares only about the blocks that one skips.
//
// A block still containing `{{` is NOT empty — it resolves on the managed cluster.
// Only a block that is empty after hub resolution ships nothing.

type emptyFinding struct {
	Chart   string // stable/oadp
	Profile string // "hub" or managed-<platform>
	Policy  string // policy-oadp-storage
	Reason  string
}

func (f emptyFinding) String() string {
	return fmt.Sprintf("%s [%s] %s: %s", f.Chart, f.Profile, f.Policy, f.Reason)
}

// scanForEmptyPolicies walks one resolved multi-document stream and reports every
// ConfigurationPolicy that would apply nothing.
func scanForEmptyPolicies(chart, profile, resolved string) []emptyFinding {
	var out []emptyFinding
	if strings.TrimSpace(resolved) == "" {
		return out
	}
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break // end of stream, or a document the e2e test already reports on
		}
		if doc == nil || doc["kind"] != "Policy" {
			continue
		}
		policyName, _ := nested(doc, "metadata", "name").(string)
		spec, _ := doc["spec"].(map[string]any)
		templates, _ := spec["policy-templates"].([]any)
		for _, pt := range templates {
			ptm, _ := pt.(map[string]any)
			od, _ := ptm["objectDefinition"].(map[string]any)
			if od == nil {
				continue
			}
			// Only ConfigurationPolicy carries object-templates. An OperatorPolicy
			// describes a subscription and is never "empty" in this sense.
			if od["kind"] != "ConfigurationPolicy" {
				continue
			}
			name, _ := nested(od, "metadata", "name").(string)
			if name == "" {
				name = policyName
			}
			odSpec, _ := od["spec"].(map[string]any)
			if odSpec == nil {
				out = append(out, emptyFinding{chart, profile, name, "ConfigurationPolicy has no spec"})
				continue
			}

			raw, hasRaw := odSpec["object-templates-raw"]
			list, hasList := odSpec["object-templates"]

			switch {
			case hasRaw:
				s, _ := raw.(string)
				if strings.Contains(s, "{{") {
					continue // resolves on the managed cluster
				}
				trimmed := strings.TrimSpace(stripYAMLComments(s))
				if trimmed == "" {
					out = append(out, emptyFinding{chart, profile, name,
						"object-templates-raw resolved to nothing, so this policy applies no objects and reports Compliant"})
					continue
				}
				var items []map[string]any
				if err := yaml.Unmarshal([]byte(s), &items); err != nil {
					continue // reported by TestObjectTemplatesRaw_ParsesAsYAML
				}
				if len(items) == 0 {
					out = append(out, emptyFinding{chart, profile, name,
						"object-templates-raw parsed to an empty list, so this policy applies no objects and reports Compliant"})
				}
			case hasList:
				items, _ := list.([]any)
				if len(items) == 0 {
					out = append(out, emptyFinding{chart, profile, name,
						"object-templates is an empty list, so this policy applies no objects and reports Compliant"})
				}
			default:
				out = append(out, emptyFinding{chart, profile, name,
					"ConfigurationPolicy has neither object-templates nor object-templates-raw"})
			}
		}
	}
	return out
}

// stripYAMLComments removes whole-line comments so a block containing only the
// indentation-anchor comment counts as empty, which is what it is.
func stripYAMLComments(s string) string {
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// reportEmptyConfigurationPolicies checks every resolved document already produced by the
// pipeline and returns the number of policies that apply nothing in EVERY profile.
//
// Called from TestPipeline_EndToEnd, which already holds the results for the hub profile
// and every managed profile. Running the pipeline again would double the slowest part of
// the suite.
//
// A policy empty in SOME profiles is behaving correctly — plenty are gated on the platform
// or on a hub-only feature, and rendering nothing where they do not apply is the point. A
// policy empty in EVERY profile is the finding: as configured by the example values it can
// never apply anything, so either its config is absent from _example.yaml (and it is
// therefore untested) or it cannot render and needs fixing.
func reportEmptyConfigurationPolicies(
	t *testing.T,
	results []ChartResult,
	extraCtxs []NamedContext,
) int {
	t.Helper()

	type key struct{ chart, policy string }
	emptyIn := map[key]map[string]string{} // key -> profile -> reason
	seenIn := map[key]map[string]bool{}    // key -> profiles where the policy rendered at all

	record := func(chart, profile, resolved string) {
		for _, name := range configurationPolicyNames(resolved) {
			k := key{chart, name}
			if seenIn[k] == nil {
				seenIn[k] = map[string]bool{}
			}
			seenIn[k][profile] = true
		}
		for _, f := range scanForEmptyPolicies(chart, profile, resolved) {
			k := key{f.Chart, f.Policy}
			if emptyIn[k] == nil {
				emptyIn[k] = map[string]string{}
			}
			emptyIn[k][f.Profile] = f.Reason
		}
	}

	for _, res := range results {
		if res.Err != nil {
			continue // helm failure, already reported
		}
		if res.ResolveOK {
			record(res.Policy, "hub", res.ResolvedYAML)
		}
		for _, ec := range extraCtxs {
			if cr := res.ExtraResults[ec.Name]; cr.ResolveOK {
				record(res.Policy, ec.Name, cr.ResolvedYAML)
			}
		}
	}

	var alwaysEmpty, conditional []string
	for k, profiles := range emptyIn {
		id := k.chart + " " + k.policy
		if len(profiles) < len(seenIn[k]) {
			conditional = append(conditional,
				fmt.Sprintf("%s (empty in %d of %d profiles)", id, len(profiles), len(seenIn[k])))
			continue
		}
		var reason string
		for _, r := range profiles {
			reason = r
			break
		}
		alwaysEmpty = append(alwaysEmpty, fmt.Sprintf("%s: %s", id, reason))
	}
	sort.Strings(alwaysEmpty)
	sort.Strings(conditional)

	for _, s := range conditional {
		t.Logf("conditional  %s — renders in at least one profile, so this is expected", s)
	}
	t.Logf("empty policies: %d conditional (expected), %d failing", len(conditional), len(alwaysEmpty))

	for _, s := range alwaysEmpty {
		t.Errorf(`FAIL  %s
	       This ConfigurationPolicy applies no objects in any cluster profile, and a
	       ConfigurationPolicy with nothing to apply reports Compliant. Either the config
	       that drives it is missing from autoshift/values/clustersets/_example.yaml, in
	       which case declare it so the branch is exercised, or the policy cannot render
	       anything and needs fixing.`, s)
	}

	return len(alwaysEmpty)
}

// configurationPolicyNames lists every ConfigurationPolicy in a resolved stream, so a
// policy can be told apart from one that did not render in a given profile at all.
func configurationPolicyNames(resolved string) []string {
	var out []string
	if strings.TrimSpace(resolved) == "" {
		return out
	}
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		if doc == nil || doc["kind"] != "Policy" {
			continue
		}
		policyName, _ := nested(doc, "metadata", "name").(string)
		spec, _ := doc["spec"].(map[string]any)
		templates, _ := spec["policy-templates"].([]any)
		for _, pt := range templates {
			ptm, _ := pt.(map[string]any)
			od, _ := ptm["objectDefinition"].(map[string]any)
			if od == nil || od["kind"] != "ConfigurationPolicy" {
				continue
			}
			name, _ := nested(od, "metadata", "name").(string)
			if name == "" {
				name = policyName
			}
			out = append(out, name)
		}
	}
	return out
}
