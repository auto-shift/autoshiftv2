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

// Without a service account, the only cluster-scoped object a hub lookup may read is the target's own
// ManagedCluster. The seed is served as local resources, which go-template-utils answers before it
// checks the allowlist, so this guards the resolver's own enforcement.
func TestHubLookupClusterScopedAllowList(t *testing.T) {
	cluster := func(name string) unstructured.Unstructured {
		return unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "cluster.open-cluster-management.io/v1",
			"kind":       "ManagedCluster",
			"metadata":   map[string]interface{}{"name": name},
		}}
	}
	r, err := NewResolver([]unstructured.Unstructured{cluster("lint-cluster"), cluster("other-cluster")})
	if err != nil {
		t.Fatal(err)
	}

	policy := func(hubOptions, expr string) string {
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
          key: '{{hub ` + expr + ` hub}}'
`
	}
	const sa = `
  hubTemplateOptions:
    serviceAccountName: autoshift-policy-service-account`
	ctx := HubContext{ManagedClusterName: "lint-cluster"}

	cases := []struct {
		name, hubOptions, expr, want string
		wantErr                      bool
	}{
		{"own cluster", "", `(lookup "cluster.open-cluster-management.io/v1" "ManagedCluster" "" "lint-cluster").metadata.name`, "key: lint-cluster", false},
		{"other cluster", "", `(lookup "cluster.open-cluster-management.io/v1" "ManagedCluster" "" "other-cluster").metadata.name`, "", true},
		{"cluster list", "", `len (lookup "cluster.open-cluster-management.io/v1" "ManagedCluster" "" "").items`, "", true},
		{"other cluster with a service account", sa, `(lookup "cluster.open-cluster-management.io/v1" "ManagedCluster" "" "other-cluster").metadata.name`, "key: other-cluster", false},
		{"cluster list with a service account", sa, `len (lookup "cluster.open-cluster-management.io/v1" "ManagedCluster" "" "").items`, `key: "2"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := r.ResolvePolicy(policy(tc.hubOptions, tc.expr), ctx)
			if tc.wantErr {
				if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "is not allowed") {
					t.Errorf("want a cluster-scoped restriction error, got %v", res.Errors)
				}
				return
			}
			if len(res.Errors) > 0 {
				t.Fatalf("want no error, got %v", res.Errors)
			}
			if !strings.Contains(res.Resolved, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, res.Resolved)
			}
		})
	}
}
