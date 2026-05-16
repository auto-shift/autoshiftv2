package resolver

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestStripDefaultsExposesGap verifies the full pipeline failure path:
// when a template reads a config key via | default "fallback" and that key
// is absent from the example file, stripStringDefaults + validateYAML must
// catch it rather than silently producing garbage output.
func TestStripDefaultsExposesGap(t *testing.T) {
	// A minimal Policy containing a ConfigurationPolicy whose
	// object-templates-raw reads "platform" from a ConfigMap.
	// | default "baremetal" would normally hide a missing "platform" key.
	const policyYAML = `
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: test-policy
  namespace: policies-autoshift
spec:
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: test-policy
        spec:
          remediationAction: enforce
          severity: low
          object-templates-raw: |
            {{- $cms := (lookup "v1" "ConfigMap" "policies-autoshift" "" "autoshift.io/rendered-config-map") | default dict }}
            {{- range $_, $cm := ($cms.items | default list) }}
            {{- $cfg := ($cm.data.config | default "" | fromYaml) }}
            {{- $ci := (index $cfg "clusterInstall" | default dict) }}
            {{- $platform := (index $ci "platform" | default "baremetal") }}
            - complianceType: musthave
              objectDefinition:
                apiVersion: v1
                kind: ConfigMap
                metadata:
                  name: test-out
                data:
                  platform: {{ $platform }}
            {{- end }}
`

	// Helper: build a synthetic rendered-config ConfigMap seeded with the
	// given config map (JSON string).
	makeRenderedCM := func(configJSON string) unstructured.Unstructured {
		configData, _ := json.Marshal(map[string]interface{}{})
		if configJSON != "" {
			configData = []byte(configJSON)
		}
		return unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "lint-cluster.rendered-config",
					"namespace": "policies-autoshift",
					"labels": map[string]interface{}{
						"autoshift.io/rendered-config-map": "",
					},
				},
				"data": map[string]interface{}{
					"config": string(configData),
				},
			},
		}
	}

	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: map[string]string{},
	}

	t.Run("key present in config — no gap, output is clean", func(t *testing.T) {
		cm := makeRenderedCM(`{"clusterInstall":{"platform":"baremetal","createCluster":"true"}}`)
		spokeR, err := NewSpokeResolver([]unstructured.Unstructured{cm})
		if err != nil {
			t.Fatalf("NewSpokeResolver: %v", err)
		}

		stripped := stripStringDefaults(policyYAML)
		result := spokeR.ResolveSpokeTemplates(stripped, ctx)
		if len(result.Errors) > 0 {
			t.Fatalf("unexpected spoke errors: %v", result.Errors)
		}
		errs := validateYAML(result.Resolved)
		if len(errs) > 0 {
			t.Errorf("unexpected validation errors: %v", errs)
		}
		if strings.Contains(result.Resolved, "<no value>") {
			t.Error("unexpected <no value> in output")
		}
		if !strings.Contains(result.Resolved, "platform: baremetal") {
			t.Error("expected 'platform: baremetal' in output")
		}
	})

	t.Run("key missing from config — gap detected via <no value>", func(t *testing.T) {
		// Config has clusterInstall but no "platform" key.
		cm := makeRenderedCM(`{"clusterInstall":{"createCluster":"true"}}`)
		spokeR, err := NewSpokeResolver([]unstructured.Unstructured{cm})
		if err != nil {
			t.Fatalf("NewSpokeResolver: %v", err)
		}

		stripped := stripStringDefaults(policyYAML)
		result := spokeR.ResolveSpokeTemplates(stripped, ctx)
		// Spoke resolution itself may or may not error — what matters is
		// that validateYAML flags the <no value> placeholder.
		combined := result.Resolved
		if combined == "" {
			combined = policyYAML // fallback: validate original if no output
		}

		errs := validateYAML(combined)
		foundGap := false
		for _, e := range errs {
			if strings.Contains(e, "<no value>") {
				foundGap = true
				t.Logf("gap correctly detected: %s", e)
			}
		}
		if !foundGap {
			// Also check raw output for <no value> in case validateYAML
			// didn't run (e.g. spoke resolution errored before producing output).
			if strings.Contains(combined, "<no value>") {
				t.Logf("gap detected in raw output (validateYAML skipped due to spoke error)")
				foundGap = true
			}
		}
		if !foundGap {
			t.Error("expected gap to be detected (<no value> or validation error) but test passed cleanly — stripStringDefaults is not working")
		}
	})

	t.Run("without stripping — missing key silently uses default", func(t *testing.T) {
		// Same missing-key config, but WITHOUT stripping defaults.
		// The output should be clean (falsely passing) to confirm that
		// stripping is what makes the difference.
		cm := makeRenderedCM(`{"clusterInstall":{"createCluster":"true"}}`)
		spokeR, err := NewSpokeResolver([]unstructured.Unstructured{cm})
		if err != nil {
			t.Fatalf("NewSpokeResolver: %v", err)
		}

		// No stripping — use raw policy YAML.
		result := spokeR.ResolveSpokeTemplates(policyYAML, ctx)
		if len(result.Errors) > 0 {
			t.Logf("(spoke errors without stripping: %v)", result.Errors)
		}
		errs := validateYAML(result.Resolved)
		hasNoValue := strings.Contains(result.Resolved, "<no value>")
		hasValidationErr := len(errs) > 0

		if hasNoValue || hasValidationErr {
			t.Logf("NOTE: even without stripping, gap was detected — default may already be absent")
		} else {
			t.Logf("confirmed: without stripping, missing key silently uses fallback (no gap detected)")
			if !strings.Contains(result.Resolved, "platform: baremetal") {
				t.Error("expected fallback 'baremetal' in unstripped output")
			}
		}
	})
}
