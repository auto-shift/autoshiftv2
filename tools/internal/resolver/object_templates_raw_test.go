//go:build integration

package resolver

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// TestObjectTemplatesRaw_ParsesAsYAML closes a gap in the end-to-end pipeline: that
// test validates the resolved Policy document, but an object-templates-raw block is
// a *string* at that level, so whatever YAML it contains is never parsed. A raw block
// can therefore be structurally broken while the Policy around it stays perfectly
// valid.
//
// The failure that motivated this: an unquoted `key: {{hub … | toLiteral hub}}`
// swallows the newline after it, so the next key lands on the same line
// (`secrets: [...]remoteWrite:`). The Policy still parses; the object the policy
// actually applies does not.
//
// Blocks that still contain Go template markers after resolution are skipped — they
// are meant to be evaluated later, on the managed cluster.
func TestObjectTemplatesRaw_ParsesAsYAML(t *testing.T) {
	root := repoRoot(t)
	policiesDir := filepath.Join(root, "policies")
	valuesDir := filepath.Join(root, "autoshift", "values")
	testdataDir := filepath.Join(root, "tools", "testdata")

	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: BuildSyntheticLabels(declared),
	}
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	syntheticCMs, err := GenerateSyntheticConfigMaps(configs, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	testResources, err := LoadTestResources(testdataDir)
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	seed := append(syntheticCMs, testResources...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	_, results, err := RunPipeline(policiesDir, ctx, nil, r, spokeR, declared, configs, testdataDir)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	checked, skipped, failures := 0, 0, 0
	for _, res := range results {
		if res.Err != nil || res.ResolvedYAML == "" {
			continue
		}
		dec := yaml.NewDecoder(strings.NewReader(res.ResolvedYAML))
		for {
			var doc map[string]any
			if err := dec.Decode(&doc); err != nil {
				break // end of stream, or a doc the e2e test already reports on
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
				odSpec, _ := od["spec"].(map[string]any)
				raw, ok := odSpec["object-templates-raw"].(string)
				if !ok || strings.TrimSpace(raw) == "" {
					continue
				}
				if strings.Contains(raw, "{{") {
					skipped++
					continue
				}
				checked++
				var items []map[string]any
				if err := yaml.Unmarshal([]byte(raw), &items); err != nil {
					failures++
					t.Errorf("%s: policy %s: object-templates-raw is not parseable YAML: %v\n--- block ---\n%s",
						res.Policy, policyName, err, indent(raw))
					continue
				}
				for i, it := range items {
					if _, has := it["objectDefinition"]; !has {
						failures++
						t.Errorf("%s: policy %s: object-templates-raw entry %d has no objectDefinition (keys: %v)",
							res.Policy, policyName, i, keysOf(it))
					}
				}
			}
		}
	}
	t.Logf("object-templates-raw: %d blocks parsed, %d skipped (still templated), %d failures", checked, skipped, failures)
	if checked == 0 {
		t.Fatal("no object-templates-raw blocks were checked — the harness is not seeing resolved output")
	}
}

func nested(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}
