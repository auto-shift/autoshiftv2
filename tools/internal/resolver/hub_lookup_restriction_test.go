package resolver

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A hub lookup outside the policy namespace resolves only when the Policy names a service account in
// spec.hubTemplateOptions, as on a real hub.
func TestHubLookupRestriction(t *testing.T) {
	cm := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "outside",
			"namespace": "other-namespace",
		},
		"data": map[string]interface{}{"key": "value"},
	}}
	r, err := NewResolver([]unstructured.Unstructured{cm})
	if err != nil {
		t.Fatal(err)
	}

	policy := func(hubOptions string) string {
		return `apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-lookup
  namespace: policies-test
spec:` + hubOptions + `
  policy-templates:
    - objectDefinition:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: copy
        data:
          key: '{{hub fromConfigMap "other-namespace" "outside" "key" hub}}'
`
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster"}

	res := r.ResolvePolicy(policy(""), ctx)
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "restricted to policies-test") {
		t.Errorf("without a service account: want a namespace restriction error, got %v", res.Errors)
	}

	res = r.ResolvePolicy(policy(`
  hubTemplateOptions:
    serviceAccountName: autoshift-policy-service-account`), ctx)
	if len(res.Errors) > 0 {
		t.Errorf("with a service account: want no error, got %v", res.Errors)
	}
	if !strings.Contains(res.Resolved, "key: value") {
		t.Errorf("with a service account: lookup did not resolve:\n%s", res.Resolved)
	}
}
