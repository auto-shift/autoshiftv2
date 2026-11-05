//go:build integration

package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

// policyMutationCases enumerates the ways a policy goes wrong. Each one breaks a real policy and
// states the substrings the resulting diagnostic must contain.
func policyMutationCases() []policyMutation {
	return []policyMutation{
		{
			name:   "missing_testdata_stub_makes_policy_empty",
			policy: "stable/file-integrity",
			note:   "a policy reads a hub ConfigMap that has no stub in tools/testdata/, so it renders nothing",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				if err := os.Remove(filepath.Join(testdataDir, "file-integrity-aide-config.yaml")); err != nil {
					t.Fatalf("remove stub: %v", err)
				}
			},
			want: []string{"empty policy", "applies no objects", "manifests/aide-config.yaml"},
		},
		{
			name:   "in_template_gate_repeats_its_own_placement",
			policy: "stable/openshift-image-registry",
			note:   "a conditional duplicating the policy's placement, so the policy applies nothing where the gate is false and still reports Compliant",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				f := filepath.Join(policyDir, "manifests", "image-registry-storage.yaml")
				editFile(t, f, "object-templates-raw: |\n",
					"object-templates-raw: |\n  {{hub- if eq (index .ManagedClusterLabels \"autoshift.io/imageregistry-storage-type\" | default \"\") \"pvc\" hub}}\n")
				body, err := os.ReadFile(f)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if err := os.WriteFile(f, append(body, []byte("  {{hub- end hub}}\n")...), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			want: []string{"empty policy", "applies no objects", "manifests/image-registry-storage.yaml"},
		},
		{
			name:   "label_consumed_but_never_declared",
			policy: "stable/openshift-image-registry",
			note:   "a hub template reads an autoshift.io label that no _example file declares",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "manifests", "image-registry-storage.yaml"),
					"autoshift.io/imageregistry-pvc-size", "autoshift.io/imageregistry-pvc-totally-made-up")
			},
			want: []string{"label contract", "imageregistry-pvc-totally-made-up", "not declared"},
		},
		{
			name:   "label_typo_shadowed_by_a_longer_declared_label",
			policy: "stable/openshift-image-registry",
			note:   "a misspelled label that a longer declared label extends with a hyphen (imageregistry-pvc-storage against the declared imageregistry-pvc-storage-class)",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "manifests", "image-registry-storage.yaml"),
					"autoshift.io/imageregistry-pvc-size", "autoshift.io/imageregistry-pvc-storage")
			},
			want: []string{"label contract", "imageregistry-pvc-storage ", "not declared"},
		},
		{
			name:   "policy_name_too_long_to_replicate",
			policy: "stable/cluster-set-assignment",
			note:   "a Policy name over 40 characters, which with a 20-character namespace exceeds the 62-character limit on the replicated name",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "policy-generator-config.yaml"),
					"  - name: policy-cluster-set-assignment",
					"  - name: policy-cluster-set-assignment-with-a-name-far-too-long")
			},
			want: []string{"policy name", "characters (max 40)"},
		},
		{
			name:   "trim_marker_dedented_out_of_its_block_scalar",
			policy: "stable/file-integrity",
			note:   "a {{- directive indented shallower than the block scalar content, which trims past the newline into the previous line",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "manifests", "aide-config.yaml"),
					"\n  {{- range $inst := (dig \"instances\" (list) $spoke) }}",
					"\n{{- range $inst := (dig \"instances\" (list) $spoke) }}")
			},
			want: []string{"manifests/aide-config.yaml", "could not find expected ':'"},
		},
		{
			name:   "lookup_of_a_kind_that_does_not_exist",
			policy: "stable/file-integrity",
			note:   "a lookup naming a GVK that is not registered, which cannot resolve on a cluster either",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "manifests", "aide-config.yaml"),
					`lookup "v1" "ConfigMap" "openshift-file-integrity" $inst.name`,
					`lookup "fake.example.io/v1" "Widget" "openshift-file-integrity" $inst.name`)
			},
			want: []string{"are not installed on the API server", "fake.example.io/v1"},
		},
		{
			name:   "go_comment_inside_a_hub_template",
			policy: "stable/cluster-set-assignment",
			note:   "a Go comment inside {{hub ... hub}}, which hub templates do not support",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "manifests", "assignment.yaml"),
					"object-templates-raw: |\n",
					"object-templates-raw: |\n  {{hub /* this breaks the parse */ hub}}\n")
			},
			want: []string{`unexpected "/" in command`, "policy JSON omitted", "not in the source manifest"},
		},
		{
			name:   "helm_holdout_template_does_not_compile",
			policy: "stable/cluster-labels",
			note:   "a Helm holdout chart with a template that fails to render",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				f := filepath.Join(policyDir, "templates", "config-maps.yaml")
				body, err := os.ReadFile(f)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if err := os.WriteFile(f, append(body, []byte("\n{{ thisFunctionDoesNotExist . }}\n")...), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			want: []string{"render failed"},
		},
		{
			name:   "evaluation_interval_removed",
			policy: "stable/cluster-set-assignment",
			note:   "a ConfigurationPolicy with no evaluationInterval, which silently falls back to `watch`",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "policy-generator-config.yaml"),
					"  evaluationInterval:\n    compliant: ${EVAL_COMPLIANT}\n    noncompliant: ${EVAL_NONCOMPLIANT}\n",
					"")
			},
			want: []string{"no evaluationInterval with both compliant and noncompliant", "defaults to `watch`"},
		},
		{
			name:   "policy_directory_has_no_readme",
			policy: "stable/cert-manager",
			note:   "a policy directory with no README.md",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				if err := os.Remove(filepath.Join(policyDir, "README.md")); err != nil {
					t.Fatalf("remove README: %v", err)
				}
			},
			want: []string{"has no README.md"},
		},
		{
			name:   "manifest_path_points_at_a_missing_file",
			policy: "stable/cluster-set-assignment",
			note:   "a policies[] entry references a manifest that does not exist",
			mutate: func(t *testing.T, policyDir, testdataDir string) {
				editFile(t, filepath.Join(policyDir, "policy-generator-config.yaml"),
					"manifests/assignment.yaml", "manifests/does-not-exist.yaml")
			},
			want: []string{"render failed"},
		},
	}
}
