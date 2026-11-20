//go:build integration

package resolver

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Cost of the managed-side token-reachability closure (STRESS_SCALE=1). One live token Secret, a
// read-only Role in each of 300 namespaces, and n RoleBindings to it; every fifth names the token
// holder. Nothing is reachable, so the view grant must render. A large hub can hold thousands of
// RoleBindings and ClusterRoleBindings. Most of the elapsed time is the harness: go-template-utils
// without a DynamicWatcher dedups every listed object against all earlier ones (quadratic), which
// config-policy-controller does not do.
func TestStress_NamespaceRbacReachScale(t *testing.T) {
	t.Parallel()
	if os.Getenv("STRESS_SCALE") == "" {
		t.Skip("set STRESS_SCALE=1")
	}
	view := func(m map[string]interface{}) {
		m[nsRbacVault] = map[string]interface{}{"roleBindings": map[string]interface{}{"x-view": clusterBinding("view", "example-team/x-ok")}}
	}
	for _, n := range []int{0, 2000, 10000} {
		var b strings.Builder
		b.WriteString(`apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-r
  namespace: example-team
rules:
  - apiGroups: ['']
    resources: [configmaps]
    verbs: [get]
`)
		for i := 0; i < 300 && i < n; i++ {
			fmt.Fprintf(&b, "---\napiVersion: rbac.authorization.k8s.io/v1\nkind: Role\nmetadata:\n  name: x-r\n  namespace: ns%d\nrules:\n  - apiGroups: ['']\n    resources: [configmaps]\n    verbs: [get]\n", i)
		}
		for i := 0; i < n; i++ {
			subj := fmt.Sprintf("  - kind: ServiceAccount\n    name: sa%d\n    namespace: ns%d\n", i, i%300)
			if i%5 == 0 {
				subj += "  - kind: ServiceAccount\n    name: x-minter\n    namespace: example-vault\n"
			}
			fmt.Fprintf(&b, "---\napiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding\nmetadata:\n  name: rb%d\n  namespace: ns%d\nroleRef:\n  apiGroup: rbac.authorization.k8s.io\n  kind: Role\n  name: x-r\nsubjects:\n%s", i, i%300, subj)
		}
		start := time.Now()
		objs := runNsRbac(t, b.String(), view)
		elapsed := time.Since(start).Round(100 * time.Millisecond)
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-view"]; !ok {
			t.Errorf("n=%d: view RoleBinding refused although nothing is reachable: %v", n, nsRbacStatusData(t, objs))
		}
		t.Logf("bindings=%d elapsed=%v", n, elapsed)
	}
}
