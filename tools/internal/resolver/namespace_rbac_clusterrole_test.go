//go:build integration

package resolver

// Tests for the clusterRole allowlist of stable/namespace-rbac: a RoleBinding may
// reference the built-in ClusterRole edit or view instead of a declared Role. Every other guard
// stays: ServiceAccount subjects only, platform namespaces refused, missing subject namespaces
// dropped, foreign objects held, nothing deleted, and the token-Secret exception never widened.

import (
	"fmt"
	"strings"
	"testing"
)

// addClusterBinding declares the RoleBinding name in example-team, bound to clusterRole, with the
// subject example-team/example-ci.
func addClusterBinding(name string, b map[string]interface{}) func(nsMap map[string]interface{}) {
	return func(nsMap map[string]interface{}) {
		exampleNs(nsMap)["roleBindings"].(map[string]interface{})[name] = b
	}
}

func clusterBinding(clusterRole interface{}, subjects ...string) map[string]interface{} {
	sas := map[string]interface{}{}
	for _, s := range subjects {
		sas[s] = true
	}
	return map[string]interface{}{"clusterRole": clusterRole, "serviceAccounts": sas}
}

// edit and view render a RoleBinding whose roleRef is the ClusterRole, exact and labelled, next to
// the example objects; no error is reported.
func TestNamespaceRbac_ClusterRoleAllowed(t *testing.T) {
	t.Parallel()
	for _, cr := range []string{"edit", "view"} {
		t.Run(cr, func(t *testing.T) {
			objs := runNsRbac(t, "", addClusterBinding("x-"+cr, clusterBinding(cr, "example-team/example-ci", "example-vault/x-other")))
			if data := nsRbacStatusData(t, objs); data != nil {
				t.Fatalf("errors reported: %v", data)
			}
			rb, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-"+cr]
			if !ok {
				t.Fatalf("RoleBinding x-%s not rendered", cr)
			}
			if rb.complianceType != "mustonlyhave" || rb.item["recreateOption"] != "IfRequired" {
				t.Errorf("complianceType = %s recreateOption = %v, want mustonlyhave IfRequired", rb.complianceType, rb.item["recreateOption"])
			}
			wantRef := map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": cr}
			if asJSONString(t, rb.obj["roleRef"]) != asJSONString(t, wantRef) {
				t.Errorf("roleRef = %v, want %v", rb.obj["roleRef"], wantRef)
			}
			wantSubj := []interface{}{
				map[string]interface{}{"kind": "ServiceAccount", "name": "example-ci", "namespace": "example-team"},
				map[string]interface{}{"kind": "ServiceAccount", "name": "x-other", "namespace": "example-vault"},
			}
			if asJSONString(t, rb.obj["subjects"]) != asJSONString(t, wantSubj) {
				t.Errorf("subjects = %v", rb.obj["subjects"])
			}
			labels, _ := rb.obj["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
			if labels["autoshift.io/managed-by"] != "namespace-rbac" {
				t.Errorf("labels = %v", labels)
			}
			for k := range nsRbacRendered(objs) {
				if strings.HasPrefix(k, "ClusterRole") {
					t.Errorf("%s rendered: namespace-rbac never creates cluster-scoped RBAC", k)
				}
			}
			assertExampleIntact(t, objs)
		})
	}
}

// Everything outside the allowlist, and every subject guard, refused and reported; nothing renders
// for the refused RoleBinding, and the example objects still apply.
func TestNamespaceRbac_ClusterRoleRefusals(t *testing.T) {
	t.Parallel()
	const blocked = "RoleBinding|example-team|x-cr"
	cases := map[string]struct {
		b    map[string]interface{}
		want string
	}{
		"admin":         {clusterBinding("admin", "example-team/example-ci"), `clusterRole "admin" is not one of edit, view`},
		"cluster-admin": {clusterBinding("cluster-admin", "example-team/example-ci"), `clusterRole "cluster-admin" is not one of edit, view`},
		"self-provisioner": {clusterBinding("self-provisioner", "example-team/example-ci"),
			`clusterRole "self-provisioner" is not one of edit, view`},
		"case":       {clusterBinding("Edit", "example-team/example-ci"), `clusterRole "Edit" is not one of edit, view`},
		"padded":     {clusterBinding(" edit", "example-team/example-ci"), `clusterRole " edit" is not one of edit, view`},
		"empty":      {clusterBinding("", "example-team/example-ci"), "clusterRole is not a non-empty string"},
		"null":       {clusterBinding(nil, "example-team/example-ci"), "clusterRole is not a non-empty string"},
		"list":       {clusterBinding([]interface{}{"edit"}, "example-team/example-ci"), "clusterRole is not a non-empty string"},
		"delimiters": {clusterBinding("{{ edit }}", "example-team/example-ci"), "clusterRole contains template delimiters"},
		"both keys": {map[string]interface{}{"role": "example-ci-builds", "clusterRole": "view",
			"serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}, "role and clusterRole are mutually exclusive"},
		"platform subject":    {clusterBinding("view", "openshift-config/x"), "openshift-config is a platform namespace"},
		"kube-system subject": {clusterBinding("edit", "kube-system/x"), "kube-system is a platform namespace"},
		"default subject":     {clusterBinding("edit", "example-team/default"), "the default ServiceAccount"},
		"bad subject":         {clusterBinding("view", "example-ci"), "is not <namespace>/<serviceaccount>"},
		"groups": {map[string]interface{}{"clusterRole": "view", "serviceAccounts": []interface{}{"system:authenticated"}},
			"serviceAccounts is not a map"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			objs := runNsRbac(t, "", addClusterBinding("x-cr", c.b))
			if _, ok := nsRbacRendered(objs)[blocked]; ok {
				t.Errorf("%s rendered", blocked)
			}
			if got := nsRbacStatusData(t, objs)[nsRbacExampleNs]; !strings.Contains(got, c.want) {
				t.Errorf("status = %q, want it to contain %q", got, c.want)
			}
			assertExampleIntact(t, objs)
		})
	}
	// On a hub (nsRbacHubStubs), a subject in a ManagedCluster namespace refuses the binding.
	t.Run("cluster namespace", func(t *testing.T) {
		objs := runNsRbac(t, nsRbacHubStubs, addClusterBinding("x-cr", clusterBinding("view", "lint-cluster/x")))
		if _, ok := nsRbacRendered(objs)[blocked]; ok {
			t.Errorf("%s rendered", blocked)
		}
		if got, want := nsRbacStatusData(t, objs)[nsRbacExampleNs], "lint-cluster is the namespace of a ManagedCluster"; !strings.Contains(got, want) {
			t.Errorf("status = %q, want it to contain %q", got, want)
		}
		assertExampleIntact(t, objs)
	})
	// An entry in a platform namespace is refused as a whole, clusterRole or not.
	t.Run("platform namespace entry", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			m["openshift-config"] = map[string]interface{}{"roleBindings": map[string]interface{}{"x-cr": clusterBinding("view", "example-team/example-ci")}}
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|openshift-config|x-cr"]; ok {
			t.Error("RoleBinding rendered in a platform namespace")
		}
		if got := nsRbacStatusData(t, objs)["openshift-config"]; !strings.Contains(got, "is a platform namespace") {
			t.Errorf("status = %q", got)
		}
	})
}

// A subject in a namespace that does not exist is left out; a RoleBinding of the same name without
// the managed-by label is never taken over; a Role named like the ClusterRole has no effect on it.
func TestNamespaceRbac_ClusterRoleManagedSide(t *testing.T) {
	t.Parallel()
	t.Run("missing subject namespace", func(t *testing.T) {
		objs := runNsRbac(t, "", addClusterBinding("x-cr", clusterBinding("edit", "example-team/example-ci", "example-absent/x")))
		rb, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-cr"]
		if !ok {
			t.Fatal("RoleBinding not rendered")
		}
		if s := asJSONString(t, rb.obj["subjects"]); strings.Contains(s, "example-absent") {
			t.Errorf("subject in a missing namespace kept: %s", s)
		}
		if got := nsRbacStatusData(t, objs)[nsRbacExampleNs]; !strings.Contains(got, "subject example-absent/x is left out, because namespace example-absent does not exist") {
			t.Errorf("status = %q", got)
		}
	})
	t.Run("only missing subject namespaces", func(t *testing.T) {
		objs := runNsRbac(t, "", addClusterBinding("x-cr", clusterBinding("edit", "example-absent/x")))
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-cr"]; ok {
			t.Error("RoleBinding rendered with no subject left")
		}
	})
	t.Run("missing namespace", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			m["example-absent"] = map[string]interface{}{"roleBindings": map[string]interface{}{"x-cr": clusterBinding("edit", "example-team/example-ci")}}
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-absent|x-cr"]; ok {
			t.Error("RoleBinding rendered in a namespace that does not exist")
		}
		if got := nsRbacStatusData(t, objs)["example-absent"]; !strings.Contains(got, "namespace example-absent does not exist") {
			t.Errorf("status = %q", got)
		}
		assertExampleIntact(t, objs)
	})
	t.Run("foreign RoleBinding", func(t *testing.T) {
		stub := `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-cr
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: ServiceAccount
    name: example-other
    namespace: example-team
`
		objs := runNsRbac(t, stub, addClusterBinding("x-cr", clusterBinding("edit", "example-team/example-ci")))
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-cr"]; ok {
			t.Error("foreign RoleBinding taken over")
		}
		if got := nsRbacStatusData(t, objs)[nsRbacExampleNs]; !strings.Contains(got, "RoleBinding example-team/x-cr already exists and is not managed by namespace-rbac") {
			t.Errorf("status = %q", got)
		}
		if nsRbacStale(objs, nsRbacInformCP)["RoleBinding|example-team|x-cr"] {
			t.Error("foreign RoleBinding reported as stale")
		}
	})
	t.Run("held Role of the same name", func(t *testing.T) {
		stub := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: edit
  namespace: example-team
rules:
  - apiGroups: ['']
    resources: [configmaps]
    verbs: [get]
`
		objs := runNsRbac(t, stub, func(m map[string]interface{}) {
			exampleNs(m)["roles"].(map[string]interface{})["edit"] = map[string]interface{}{"rules": []interface{}{rule([]string{""}, []string{"configmaps"}, []string{"get"}, nil)}}
			addClusterBinding("x-cr", clusterBinding("edit", "example-team/example-ci"))(m)
		})
		rb, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-cr"]
		if !ok {
			t.Fatalf("ClusterRole binding held by an unrelated Role named edit: %v", nsRbacStatusData(t, objs))
		}
		if ref := rb.obj["roleRef"].(map[string]interface{}); ref["kind"] != "ClusterRole" {
			t.Errorf("roleRef = %v", ref)
		}
	})
}

// A removed clusterRole binding that is still live is reported by inform only, never deleted.
func TestNamespaceRbac_ClusterRoleStale(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-cr
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: edit
subjects:
  - kind: ServiceAccount
    name: example-ci
    namespace: example-team
`
	objs := runNsRbac(t, stub, nil)
	if !nsRbacStale(objs, nsRbacInformCP)["RoleBinding|example-team|x-cr"] {
		t.Error("undeclared clusterRole RoleBinding not reported")
	}
	if nsRbacStale(objs, nsRbacEnforceCP)["RoleBinding|example-team|x-cr"] {
		t.Error("the enforce ConfigurationPolicy would delete it")
	}
	objs = runNsRbac(t, stub, addClusterBinding("x-cr", clusterBinding("edit", "example-team/example-ci")))
	if nsRbacStale(objs, nsRbacInformCP)["RoleBinding|example-team|x-cr"] {
		t.Error("declared clusterRole RoleBinding reported as stale")
	}
}

// The token-Secret exception is never widened through a ClusterRole: a tokenSecret ServiceAccount
// is never a subject of edit or view, and edit is refused in a namespace with a tokenSecret
// (edit creates workloads, reads Secrets, and requests tokens for every ServiceAccount there).
func TestNamespaceRbac_ClusterRoleTokenGuard(t *testing.T) {
	t.Parallel()
	const liveToken = `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
`
	for _, cr := range []string{"edit", "view"} {
		t.Run("minter as subject of "+cr, func(t *testing.T) {
			objs := runNsRbac(t, "", func(m map[string]interface{}) {
				minterSetup(m)
				addClusterBinding("x-cr", clusterBinding(cr, "example-vault/x-minter"))(m)
			})
			got := nsRbacRendered(objs)
			for _, k := range []string{"RoleBinding|example-team|x-cr", tokenSecretKey} {
				if _, ok := got[k]; ok {
					t.Errorf("%s rendered", k)
				}
			}
			data := nsRbacStatusData(t, objs)
			if want := `serviceAccounts.example-vault/x-minter has a tokenSecret, so it can only be bound to a minter-only Role, and ClusterRole "` + cr + `" is not one`; !strings.Contains(data[nsRbacExampleNs], want) {
				t.Errorf("status[%s] = %q, want %q", nsRbacExampleNs, data[nsRbacExampleNs], want)
			}
			if want := `RoleBinding example-team/x-cr binds it to ClusterRole "` + cr + `", which is not minter-only`; !strings.Contains(data[nsRbacVault], want) {
				t.Errorf("status[%s] = %q, want %q", nsRbacVault, data[nsRbacVault], want)
			}
			assertExampleIntact(t, objs)
		})
	}
	t.Run("edit in the namespace of a tokenSecret", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roleBindings"] = map[string]interface{}{
				"x-edit": clusterBinding("edit", "example-vault/x-ok"),
				"x-view": clusterBinding("view", "example-vault/x-ok"),
			}
		})
		got := nsRbacRendered(objs)
		if _, ok := got["RoleBinding|example-vault|x-edit"]; ok {
			t.Error("edit RoleBinding rendered next to a tokenSecret")
		}
		if _, ok := got["RoleBinding|example-vault|x-view"]; !ok {
			t.Error("view RoleBinding not rendered: view reads no Secrets and creates no workloads")
		}
		if _, ok := got[tokenSecretKey]; !ok {
			t.Error("the accepted token Secret is no longer rendered")
		}
		want := "clusterRole edit is refused in namespace example-vault, which has ServiceAccounts with a tokenSecret (x-minter)"
		if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, want) {
			t.Errorf("status = %q, want %q", st, want)
		}
	})
	t.Run("edit next to a refused tokenSecret", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roleBindings"].(map[string]interface{})["x-mint"].(map[string]interface{})["role"] = "example-ci-builds"
			vaultNs(m)["roleBindings"] = map[string]interface{}{"x-edit": clusterBinding("edit", "example-team/example-ci")}
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-edit"]; ok {
			t.Error("edit RoleBinding rendered next to a declared (refused) tokenSecret")
		}
	})
	t.Run("edit next to a live token Secret no longer declared", func(t *testing.T) {
		objs := runNsRbac(t, liveToken, func(m map[string]interface{}) {
			m[nsRbacVault] = map[string]interface{}{
				"serviceAccounts": map[string]interface{}{"x-minter": true},
				"roleBindings":    map[string]interface{}{"x-edit": clusterBinding("edit", "example-team/example-ci")},
			}
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-edit"]; ok {
			t.Error("edit RoleBinding rendered while a live token Secret is in its namespace")
		}
		want := "RoleBinding example-vault/x-edit is not created or changed: it binds ClusterRole edit in a namespace that holds long-lived token Secrets of x-minter"
		if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, want) {
			t.Errorf("status = %q, want %q", st, want)
		}
		if !nsRbacStale(objs, nsRbacInformCP)[tokenSecretKey] {
			t.Error("the live token Secret is not reported")
		}
	})
	t.Run("live edit RoleBinding next to a pending token Secret", func(t *testing.T) {
		stub := `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-edit
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: edit
subjects:
  - kind: ServiceAccount
    name: example-ci
    namespace: example-team
`
		objs := runNsRbac(t, stub, minterSetup)
		if _, ok := nsRbacRendered(objs)[tokenSecretKey]; ok {
			t.Error("token Secret rendered next to a live edit RoleBinding")
		}
		want := "RoleBinding example-vault/x-edit binds ClusterRole edit in the namespace of the Secret"
		if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, want) {
			t.Errorf("status = %q, want %q", st, want)
		}
		if nsRbacStale(objs, nsRbacEnforceCP)["RoleBinding|example-vault|x-edit"] {
			t.Error("the enforce ConfigurationPolicy would delete the live RoleBinding")
		}
	})
	t.Run("edit subject next to a live token of the subject", func(t *testing.T) {
		// The subject has a live labelled token Secret (its tokenSecret was removed): an edit or view
		// RoleBinding naming it is held like any binding to a non-minter Role.
		objs := runNsRbac(t, liveToken, func(m map[string]interface{}) {
			m[nsRbacVault] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"x-minter": true}}
			addClusterBinding("x-cr", clusterBinding("view", "example-vault/x-minter"))(m)
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-cr"]; ok {
			t.Error("view RoleBinding rendered for a ServiceAccount with a live token Secret")
		}
		want := "subject example-vault/x-minter has a long-lived token Secret managed by namespace-rbac, and ClusterRole view is not minter-only"
		if st := nsRbacStatusData(t, objs)[nsRbacExampleNs]; !strings.Contains(st, want) {
			t.Errorf("status = %q, want %q", st, want)
		}
	})
}

// Template payloads in clusterRole: refused as template delimiters and never rendered.
func TestNamespaceRbac_ClusterRoleInjection(t *testing.T) {
	t.Parallel()
	payloads := append([]string{
		`"}} {{ fail "pwn" }} {{hub fail "pwn" hub}}\` + "\n",
		"{{ (lookup `v1` `Secret` `openshift-config` `pull-secret`).data }}",
	}, preprocessPayloads...)
	for i, p := range payloads {
		objs := runNsRbac(t, "", addClusterBinding("x-cr", clusterBinding(p, "example-team/example-ci")))
		data := nsRbacStatusData(t, objs)
		if !strings.Contains(asJSONString(t, data), "x-cr") {
			t.Errorf("payload %d not reported: %v", i, data)
		}
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-cr"]; ok {
			t.Errorf("payload %d rendered a RoleBinding", i)
		}
		assertExampleIntact(t, objs)
	}
}

// A ServiceAccount that a tokenSecret ServiceAccount can mint tokens for acts with the rights of the
// long-lived token, so it is token-reachable and never bound to edit or view. The set
// comes from the Roles bound to the token ServiceAccount: serviceaccounts/token resourceNames, or
// the whole namespace when there are none.
func TestNamespaceRbac_ClusterRoleMintedFor(t *testing.T) {
	t.Parallel()
	for _, cr := range []string{"edit", "view"} {
		t.Run("minted-for subject of "+cr, func(t *testing.T) {
			objs := runNsRbac(t, "", func(m map[string]interface{}) {
				minterSetup(m)
				addClusterBinding("x-cr", clusterBinding(cr, "example-team/example-ci"))(m)
			})
			if _, ok := nsRbacRendered(objs)["RoleBinding|example-team|x-cr"]; ok {
				t.Error("RoleBinding rendered for a ServiceAccount the minter can mint tokens for")
			}
			want := "serviceAccounts.example-team/example-ci is never bound to a ClusterRole: whoever holds the long-lived token of example-vault/x-minter can act as it"
			if st := nsRbacStatusData(t, objs)[nsRbacExampleNs]; !strings.Contains(st, want) {
				t.Errorf("status = %q, want %q", st, want)
			}
			if _, ok := nsRbacRendered(objs)[tokenSecretKey]; !ok {
				t.Error("the accepted token Secret is no longer rendered")
			}
		})
	}
	t.Run("other subjects still accepted", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roleBindings"] = map[string]interface{}{"x-view": clusterBinding("view", "example-vault/x-ok")}
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-view"]; !ok {
			t.Errorf("view RoleBinding for a ServiceAccount nobody mints for is refused: %v", nsRbacStatusData(t, objs))
		}
	})
	t.Run("wildcard minter reaches every ServiceAccount of its namespace", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roles"].(map[string]interface{})["x-mint"] = map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, nil),
			}}
			vaultNs(m)["roleBindings"] = map[string]interface{}{
				"x-a": clusterBinding("view", "example-team/x-any"),
				"x-b": clusterBinding("view", "example-team/example-ci"),
				"x-c": clusterBinding("view", "example-vault/x-ok"),
			}
		})
		got := nsRbacRendered(objs)
		for _, k := range []string{"RoleBinding|example-vault|x-a", "RoleBinding|example-vault|x-b"} {
			if _, ok := got[k]; ok {
				t.Errorf("%s rendered for a ServiceAccount a wildcard minter reaches", k)
			}
		}
		if _, ok := got["RoleBinding|example-vault|x-c"]; !ok {
			t.Error("a ServiceAccount outside the wildcard minter's namespace is refused")
		}
		if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, "serviceAccounts.example-team/x-any is never bound to a ClusterRole") {
			t.Errorf("status = %q", st)
		}
	})
	// Managed side, live state only: a live labelled token Secret of x-minter, bound by a live
	// RoleBinding to a live Role that mints example-ci tokens (its tokenSecret no longer declared).
	liveMinter := `apiVersion: v1
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
  name: x-live-mint
  namespace: example-team
rules:
  - apiGroups: ['']
    resources: [serviceaccounts/token]
    verbs: [create]
    resourceNames: [example-ci]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-live-mint
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: x-live-mint
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`
	t.Run("live minter holds the binding", func(t *testing.T) {
		objs := runNsRbac(t, liveMinter, func(m map[string]interface{}) {
			vaultNs := map[string]interface{}{"roleBindings": map[string]interface{}{"x-view": clusterBinding("view", "example-team/example-ci", "example-vault/x-ok")}}
			m[nsRbacVault] = vaultNs
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-view"]; ok {
			t.Error("RoleBinding rendered for a ServiceAccount a live token can mint tokens for")
		}
		want := "RoleBinding example-vault/x-view is not created or changed: it binds ClusterRole view to example-team/example-ci (by example-vault/x-minter)"
		if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, want) {
			t.Errorf("status = %q, want %q", st, want)
		}
	})
	t.Run("live ClusterRole binding of a minted-for subject refuses the token Secret", func(t *testing.T) {
		for _, stub := range []string{`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-hand-view
  namespace: example-vault
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: ServiceAccount
    name: example-ci
    namespace: example-team
`, `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: x-hand-edit
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: edit
subjects:
  - kind: User
    name: system:serviceaccount:example-team:example-ci
`} {
			objs := runNsRbac(t, stub, minterSetup)
			if _, ok := nsRbacRendered(objs)[tokenSecretKey]; ok {
				t.Error("token Secret rendered while a minted-for ServiceAccount holds a ClusterRole")
			}
			if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, "example-team/example-ci, which the ServiceAccount can act as") {
				t.Errorf("status = %q", st)
			}
		}
	})
}

// Reachability is a closure over what the holder of the long-lived token can act as. A
// namespace becomes reachable when a reachable identity can create pods or workload controllers,
// secrets, or serviceaccounts/token there, or can impersonate; the token ServiceAccount's own
// namespace is not reachable by co-location.
func TestNamespaceRbac_ReachClosure(t *testing.T) {
	t.Parallel()
	refused := func(t *testing.T, objs []onboardingObject, key, ns, want string) {
		t.Helper()
		if _, ok := nsRbacRendered(objs)[key]; ok {
			t.Errorf("%s rendered", key)
		}
		if st := nsRbacStatusData(t, objs)[ns]; !strings.Contains(st, want) {
			t.Errorf("status[%s] = %q, want %q", ns, st, want)
		}
	}
	accepted := func(t *testing.T, objs []onboardingObject, key string) {
		t.Helper()
		if _, ok := nsRbacRendered(objs)[key]; !ok {
			t.Errorf("%s refused: %v", key, nsRbacStatusData(t, objs))
		}
	}
	vaultView := func(m map[string]interface{}, subjects ...string) {
		rbs := map[string]interface{}{}
		for i, s := range subjects {
			rbs[fmt.Sprintf("x-view-%d", i)] = clusterBinding("view", s)
		}
		vaultNs(m)["roleBindings"] = rbs
	}
	t.Run("token namespace not reachable by co-location", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			vaultView(m, "example-vault/x-ok")
		})
		accepted(t, objs, "RoleBinding|example-vault|x-view-0")
	})
	t.Run("2-hop token chain", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roles"] = map[string]interface{}{"x-hop": map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"x-y"}),
			}}}
			vaultView(m, "example-vault/x-y", "example-vault/x-ok")
			vaultNs(m)["roleBindings"].(map[string]interface{})["x-hop"] = map[string]interface{}{"role": "x-hop",
				"serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}
		})
		refused(t, objs, "RoleBinding|example-vault|x-view-0", nsRbacVault,
			"serviceAccounts.example-vault/x-y is never bound to a ClusterRole: whoever holds the long-lived token of example-vault/x-minter can act as it")
		accepted(t, objs, "RoleBinding|example-vault|x-view-1")
	})
	t.Run("pod create reaches the whole namespace", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roles"] = map[string]interface{}{"x-pods": map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"pods"}, []string{"create"}, nil),
			}}}
			vaultView(m, "example-vault/x-ok")
			vaultNs(m)["roleBindings"].(map[string]interface{})["x-pods"] = map[string]interface{}{"role": "x-pods",
				"serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}
		})
		refused(t, objs, "RoleBinding|example-vault|x-view-0", nsRbacVault, "serviceAccounts.example-vault/x-ok is never bound to a ClusterRole")
	})
	t.Run("workload controller reaches the whole namespace", func(t *testing.T) {
		// _example: example-ci holds buildconfigs create/patch in example-team, so once x-minter
		// mints example-ci, every ServiceAccount of example-team (builder included) is reachable.
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			minterSetup(m)
			vaultView(m, "example-team/builder", "example-team/x-any")
		})
		refused(t, objs, "RoleBinding|example-vault|x-view-0", nsRbacVault, "serviceAccounts.example-team/builder is never bound to a ClusterRole")
		refused(t, objs, "RoleBinding|example-vault|x-view-1", nsRbacVault, "serviceAccounts.example-team/x-any is never bound to a ClusterRole")
	})
	t.Run("fixpoint not converged fails closed", func(t *testing.T) {
		// x-minter mints example-chain/s9, s9 mints s8, ... down to s1. Refs are walked in sorted
		// order, so each round adds one hop: 9 hops do not converge within 6 rounds.
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			sas := map[string]interface{}{}
			roles := map[string]interface{}{"x-mint": map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"s9"}),
			}}}
			rbs := map[string]interface{}{"x-mint": map[string]interface{}{"role": "x-mint",
				"serviceAccounts": map[string]interface{}{"example-vault/x-minter": true}}}
			for i := 9; i >= 1; i-- {
				sas[fmt.Sprintf("s%d", i)] = true
				if i > 1 {
					rn := fmt.Sprintf("hop%d", i)
					roles[rn] = map[string]interface{}{"rules": []interface{}{
						rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{fmt.Sprintf("s%d", i-1)}),
					}}
					rbs[rn] = map[string]interface{}{"role": rn,
						"serviceAccounts": map[string]interface{}{fmt.Sprintf("example-chain/s%d", i): true}}
				}
			}
			m["example-chain"] = map[string]interface{}{"serviceAccounts": sas, "roles": roles, "roleBindings": rbs}
			m[nsRbacVault] = map[string]interface{}{
				"serviceAccounts": map[string]interface{}{"x-minter": map[string]interface{}{"tokenSecret": "x-minter-token"}},
				"roleBindings":    map[string]interface{}{"x-view-0": clusterBinding("view", "example-vault/x-ok")},
			}
		})
		refused(t, objs, "RoleBinding|example-vault|x-view-0", nsRbacVault,
			"serviceAccounts.example-vault/x-ok: token reachability did not converge within 6 rounds, so no ClusterRole is bound (fail closed)")
	})
	t.Run("short chain converges", func(t *testing.T) {
		objs := runNsRbac(t, "", func(m map[string]interface{}) {
			m["example-chain"] = map[string]interface{}{
				"serviceAccounts": map[string]interface{}{"s1": true, "s2": true},
				"roles": map[string]interface{}{
					"x-mint": map[string]interface{}{"rules": []interface{}{rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"s2"})}},
					"hop2":   map[string]interface{}{"rules": []interface{}{rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"s1"})}},
				},
				"roleBindings": map[string]interface{}{
					"x-mint": map[string]interface{}{"role": "x-mint", "serviceAccounts": map[string]interface{}{"example-vault/x-minter": true}},
					"hop2":   map[string]interface{}{"role": "hop2", "serviceAccounts": map[string]interface{}{"example-chain/s2": true}},
				},
			}
			m[nsRbacVault] = map[string]interface{}{
				"serviceAccounts": map[string]interface{}{"x-minter": map[string]interface{}{"tokenSecret": "x-minter-token"}},
				"roleBindings": map[string]interface{}{
					"x-view-0": clusterBinding("view", "example-vault/x-ok"),
					"x-view-1": clusterBinding("view", "example-chain/s1"),
				},
			}
		})
		accepted(t, objs, "RoleBinding|example-vault|x-view-0")
		refused(t, objs, "RoleBinding|example-vault|x-view-1", nsRbacVault, "serviceAccounts.example-chain/s1 is never bound to a ClusterRole")
	})
	// Live state only (no tokenSecret declared): a labelled token Secret of x-minter in example-vault.
	liveToken := `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
`
	liveView := func(m map[string]interface{}) {
		m[nsRbacVault] = map[string]interface{}{"roleBindings": map[string]interface{}{"x-view": clusterBinding("view", "example-team/x-ok")}}
	}
	t.Run("live control: nothing reaches example-team", func(t *testing.T) {
		accepted(t, runNsRbac(t, liveToken, liveView), "RoleBinding|example-vault|x-view")
	})
	t.Run("live impersonate", func(t *testing.T) {
		stub := liveToken + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-imp
  namespace: example-team
rules:
  - apiGroups: ['']
    resources: [serviceaccounts]
    verbs: [impersonate]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-imp
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: x-imp
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`
		refused(t, runNsRbac(t, stub, liveView), "RoleBinding|example-vault|x-view", nsRbacVault,
			"it binds ClusterRole view to example-team/x-ok (by example-vault/x-minter), which whoever holds a long-lived token Secret managed by namespace-rbac can act as")
	})
	t.Run("live secrets create", func(t *testing.T) {
		stub := liveToken + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-sec
  namespace: example-team
rules:
  - apiGroups: ['']
    resources: [secrets]
    verbs: [create, get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-sec
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: x-sec
subjects:
  - kind: User
    name: system:serviceaccount:example-vault:x-minter
`
		refused(t, runNsRbac(t, stub, liveView), "RoleBinding|example-vault|x-view", nsRbacVault, "it binds ClusterRole view to example-team/x-ok")
	})
	for _, group := range []string{"system:serviceaccounts:example-vault", "system:serviceaccounts"} {
		t.Run("live group subject "+group, func(t *testing.T) {
			stub := liveToken + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: x-podmaker
rules:
  - apiGroups: ['']
    resources: [pods]
    verbs: [create]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-group
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: x-podmaker
subjects:
  - kind: Group
    name: ` + group + "\n"
			refused(t, runNsRbac(t, stub, liveView), "RoleBinding|example-vault|x-view", nsRbacVault, "it binds ClusterRole view to example-team/x-ok")
		})
	}
	t.Run("live group binding of a reachable namespace refuses a pending token", func(t *testing.T) {
		stub := `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-group-view
  namespace: example-vault
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: Group
    name: system:serviceaccounts:example-team
`
		objs := runNsRbac(t, stub, minterSetup)
		if _, ok := nsRbacRendered(objs)[tokenSecretKey]; ok {
			t.Error("token Secret rendered while a group view binding covers a ServiceAccount it can act as")
		}
		if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, "binds ClusterRole view to Group system:serviceaccounts:example-team, which the ServiceAccount can act as") {
			t.Errorf("status = %q", st)
		}
	})
}

// Every way a live identity can read or take over a ServiceAccount of a namespace makes
// the namespace reachable. Each case binds a live Role in example-team to the live token of x-minter;
// the view grant for example-team/x-ok must then be held.
func TestNamespaceRbac_ReachRights(t *testing.T) {
	t.Parallel()
	liveToken := `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
`
	binding := func(kind, role string) string {
		return fmt.Sprintf(`---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-r
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: %s
  name: %s
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`, kind, role)
	}
	role := func(rules string) string {
		return "---\napiVersion: rbac.authorization.k8s.io/v1\nkind: Role\nmetadata:\n  name: x-r\n  namespace: example-team\nrules:\n" + rules + binding("Role", "x-r")
	}
	view := func(m map[string]interface{}) {
		m[nsRbacVault] = map[string]interface{}{"roleBindings": map[string]interface{}{"x-view": clusterBinding("view", "example-team/x-ok")}}
	}
	cases := map[string]string{
		"secrets get":               role("  - apiGroups: ['']\n    resources: [secrets]\n    verbs: [get]\n"),
		"secrets list":              role("  - apiGroups: ['']\n    resources: [secrets]\n    verbs: [list]\n"),
		"pods/exec":                 role("  - apiGroups: ['']\n    resources: [pods/exec]\n    verbs: [create, get]\n"),
		"pods/attach":               role("  - apiGroups: ['']\n    resources: [pods/attach]\n    verbs: [get]\n"),
		"pods/ephemeralcontainers":  role("  - apiGroups: ['']\n    resources: [pods/ephemeralcontainers]\n    verbs: [patch]\n"),
		"bind":                      role("  - apiGroups: [rbac.authorization.k8s.io]\n    resources: [clusterroles]\n    verbs: [bind]\n"),
		"escalate":                  role("  - apiGroups: [rbac.authorization.k8s.io]\n    resources: [roles]\n    verbs: [escalate]\n"),
		"impersonate users (local)": role("  - apiGroups: ['']\n    resources: [users, groups]\n    verbs: [impersonate]\n"),
	}
	for name, stub := range cases {
		t.Run(name, func(t *testing.T) {
			objs := runNsRbac(t, liveToken+stub, view)
			if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-view"]; ok {
				t.Errorf("view RoleBinding rendered: %s does not make example-team reachable", name)
			}
		})
	}
	t.Run("control: configmaps get", func(t *testing.T) {
		objs := runNsRbac(t, liveToken+role("  - apiGroups: ['']\n    resources: [configmaps]\n    verbs: [get]\n"), view)
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-view"]; !ok {
			t.Errorf("view RoleBinding refused for a read-only Role: %v", nsRbacStatusData(t, objs))
		}
	})
	t.Run("impersonate users cluster-wide reaches everything", func(t *testing.T) {
		stub := liveToken + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: x-imp-users
rules:
  - apiGroups: ['']
    resources: [users]
    verbs: [impersonate]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: x-imp-users
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: x-imp-users
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`
		objs := runNsRbac(t, stub, func(m map[string]interface{}) {
			m[nsRbacVault] = map[string]interface{}{"roleBindings": map[string]interface{}{"x-view": clusterBinding("view", "example-vault/x-ok")}}
		})
		if _, ok := nsRbacRendered(objs)["RoleBinding|example-vault|x-view"]; ok {
			t.Error("view RoleBinding rendered although the token can impersonate any user")
		}
	})
}

// A live hop (out of band, invisible to the hub) followed by a declared hop (not live
// yet) is closed on the managed cluster, because the declared edges are applied in every live round.
func TestNamespaceRbac_ReachLiveThenDeclared(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
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
  name: x-oob
  namespace: example-team
rules:
  - apiGroups: ['']
    resources: [pods]
    verbs: [create]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-oob
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: x-oob
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`
	objs := runNsRbac(t, stub, func(m map[string]interface{}) {
		m[nsRbacVault] = map[string]interface{}{
			"serviceAccounts": map[string]interface{}{"s1": true},
			"roles": map[string]interface{}{"m": map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"s1"}),
			}}},
			"roleBindings": map[string]interface{}{
				"m":      map[string]interface{}{"role": "m", "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}},
				"x-view": clusterBinding("view", "example-vault/s1"),
				"x-ok":   clusterBinding("view", "example-vault/x-ok"),
			},
		}
	})
	got := nsRbacRendered(objs)
	if _, ok := got["RoleBinding|example-vault|x-view"]; ok {
		t.Error("view RoleBinding for example-vault/s1 rendered: live hop then declared hop not closed")
	}
	if st := nsRbacStatusData(t, objs)[nsRbacVault]; !strings.Contains(st, "it binds ClusterRole view to example-vault/s1 (by example-vault/x-minter)") {
		t.Errorf("status = %q", st)
	}
	if _, ok := got["RoleBinding|example-vault|x-ok"]; !ok {
		t.Error("example-vault/x-ok refused: example-vault is not reachable")
	}
}
