package resolver

import (
	"strings"
	"testing"
)

func TestResolvePolicy_BasicHubTemplate(t *testing.T) {
	r, err := NewResolver(nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// A minimal Policy with one hub template expression in the subscription name.
	rawYAML := `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-test
  namespace: test-ns
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1beta1
        kind: OperatorPolicy
        metadata:
          name: install-operator-test
        spec:
          subscription:
            name: '{{hub index .ManagedClusterLabels "autoshift.io/test-subscription-name" | default "fallback" hub}}'
            channel: '{{hub index .ManagedClusterLabels "autoshift.io/test-channel" hub}}'
`

	ctx := HubContext{
		ManagedClusterName: "lint-cluster",
		ManagedClusterLabels: map[string]string{
			"autoshift.io/test-subscription-name": "my-operator",
			"autoshift.io/test-channel":           "stable-v1",
		},
	}

	result := r.ResolvePolicy(rawYAML, ctx)
	if len(result.Errors) > 0 {
		t.Fatalf("ResolvePolicy errors: %v", result.Errors)
	}
	resolved := result.Resolved

	// The hub templates should be replaced with the label values.
	if !strings.Contains(resolved, "my-operator") {
		t.Errorf("resolved output should contain 'my-operator', got:\n%s", resolved)
	}
	if !strings.Contains(resolved, "stable-v1") {
		t.Errorf("resolved output should contain 'stable-v1', got:\n%s", resolved)
	}
	// No unresolved hub expressions should remain.
	if strings.Contains(resolved, "{{hub") {
		t.Errorf("resolved output should not contain '{{hub', got:\n%s", resolved)
	}
}

func TestResolvePolicy_MissingLabelUsesDefault(t *testing.T) {
	r, err := NewResolver(nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	rawYAML := `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-test
  namespace: test-ns
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1beta1
        kind: OperatorPolicy
        metadata:
          name: install-operator-test
        spec:
          subscription:
            name: '{{hub index .ManagedClusterLabels "autoshift.io/missing-label" | default "the-fallback" hub}}'
`

	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: map[string]string{}, // no labels at all
	}

	result := r.ResolvePolicy(rawYAML, ctx)
	if len(result.Errors) > 0 {
		t.Fatalf("ResolvePolicy errors: %v", result.Errors)
	}
	resolved := result.Resolved

	// Should fall back to "the-fallback".
	if !strings.Contains(resolved, "the-fallback") {
		t.Errorf("resolved output should contain 'the-fallback', got:\n%s", resolved)
	}
}

func TestResolvePolicy_MissingLabelNoDefault_EmptyString(t *testing.T) {
	r, err := NewResolver(nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	rawYAML := `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-test
  namespace: test-ns
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1beta1
        kind: OperatorPolicy
        metadata:
          name: install-operator-test
        spec:
          subscription:
            channel: '{{hub index .ManagedClusterLabels "autoshift.io/missing-no-default" hub}}'
`

	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: map[string]string{},
	}

	result := r.ResolvePolicy(rawYAML, ctx)
	if len(result.Errors) > 0 {
		t.Fatalf("ResolvePolicy errors: %v", result.Errors)
	}
	resolved := result.Resolved

	// The missing label with no default resolves to empty string.
	// No hub template markers should remain.
	if strings.Contains(resolved, "{{hub") {
		t.Errorf("should not contain unresolved hub templates, got:\n%s", resolved)
	}
}

func TestResolvePolicy_SprigFunctions(t *testing.T) {
	r, err := NewResolver(nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// Test the ternary function (used in the disconnected-mirror source pattern).
	rawYAML := `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-test
  namespace: test-ns
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1beta1
        kind: OperatorPolicy
        metadata:
          name: test
        spec:
          subscription:
            source: '{{hub $base := (index .ManagedClusterLabels "autoshift.io/test-source" | default "redhat-operators") hub}}{{hub ternary (printf "%s-%s" $base (index .ManagedClusterLabels "autoshift.io/mirror-catalog-suffix" | default "mirror")) $base (eq (index .ManagedClusterLabels "autoshift.io/disconnected-mirror" | default "false") "true") hub}}'
`

	ctx := HubContext{
		ManagedClusterName: "lint-cluster",
		ManagedClusterLabels: map[string]string{
			"autoshift.io/test-source":          "redhat-operators",
			"autoshift.io/disconnected-mirror":  "false",
			"autoshift.io/mirror-catalog-suffix": "mirror",
		},
	}

	result := r.ResolvePolicy(rawYAML, ctx)
	if len(result.Errors) > 0 {
		t.Fatalf("ResolvePolicy errors: %v", result.Errors)
	}
	resolved := result.Resolved

	// disconnected-mirror is false, so source should be the base without suffix.
	if !strings.Contains(resolved, "redhat-operators") {
		t.Errorf("expected 'redhat-operators' in resolved output, got:\n%s", resolved)
	}
	if strings.Contains(resolved, "redhat-operators-mirror") {
		t.Errorf("should NOT contain 'redhat-operators-mirror' when disconnected-mirror=false, got:\n%s", resolved)
	}
}

func TestResolvePolicy_PassthroughNonPolicy(t *testing.T) {
	r, err := NewResolver(nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// A Placement document should pass through unchanged.
	rawYAML := `---
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: placement-test
spec:
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchExpressions:
            - key: 'autoshift.io/cert-manager'
              operator: In
              values:
              - 'true'
`

	ctx := HubContext{ManagedClusterName: "lint-cluster"}

	result := r.ResolvePolicy(rawYAML, ctx)
	if len(result.Errors) > 0 {
		t.Fatalf("ResolvePolicy errors: %v", result.Errors)
	}
	resolved := result.Resolved

	// Placement should be in the output as-is.
	if !strings.Contains(resolved, "autoshift.io/cert-manager") {
		t.Errorf("Placement should pass through unchanged, got:\n%s", resolved)
	}
}
