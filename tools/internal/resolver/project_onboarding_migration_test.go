//go:build integration

package resolver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Migration from the single policy-project-onboarding Policy to the shard Policies. The golden file
// was written from the single-policy template, before sharding, for the deployment-shaped projects
// of tools/testdata/project-onboarding-migration/projects.yaml, with their live state as the old Policy left it
// (adopted Namespaces and RoleBindings). The sharded template must render the same objects with the
// same content and remediationAction, under prune on and off and under dry-run: only the owning
// Policy differs. Status ConfigMaps are not compared, their names changed on purpose.
//
// Regenerate only for a deliberate change of what these projects render, never to make a sharding
// change pass: ONBOARDING_MIGRATION_GOLDEN=write go test -tags integration -run MigrationGolden.

const migrationGoldenPath = "tools/testdata/project-onboarding-migration/golden.json"

// migrationHubConfig replaces cfg with the deployment-shaped config.
func migrationHubConfig(t *testing.T) func(cfg map[string]interface{}) {
	return func(cfg map[string]interface{}) {
		hub := stressDeploymentConfig(t)
		for k := range cfg {
			delete(cfg, k)
		}
		for k, v := range hub {
			cfg[k] = v
		}
	}
}

// migrationRealProjects returns the projects of the deployment-shaped config.
func migrationRealProjects(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	out := map[string]map[string]interface{}{}
	for name, p := range stressDeploymentConfig(t)["projects"].(map[string]interface{}) {
		out[name] = p.(map[string]interface{})
	}
	if len(out) == 0 {
		t.Fatal("the deployment-shaped config declares no projects")
	}
	return out
}

// migrationLive is the live state of the deployment-shaped projects as the single Policy left it: each Namespace
// adopted (ownership labels, the project-labels annotation, plus labels and annotations other tools
// own), its group RoleBindings, and the old status ConfigMap.
func migrationLive(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	names := []string{}
	projects := migrationRealProjects(t)
	for name := range projects {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, `apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
  labels:
    kubernetes.io/metadata.name: %[1]s
    pod-security.kubernetes.io/audit: restricted
    autoshift.io/project: %[1]s
    autoshift.io/managed-by: project-onboarding
  annotations:
    openshift.io/sa.scc.mcs: s0:c27,c4
    autoshift.io/project-labels: ""
---
`, name)
		groups, _ := projects[name]["groups"].(map[string]interface{})
		for level, role := range map[string]string{"admin": "admin", "write": "edit", "read": "view"} {
			g, ok := groups[level].(string)
			if !ok {
				continue
			}
			fmt.Fprintf(&b, `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-%[2]s
  namespace: %[1]s
  labels: {autoshift.io/project: %[1]s, autoshift.io/managed-by: project-onboarding}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: %[3]s}
subjects:
  - {apiGroup: rbac.authorization.k8s.io, kind: Group, name: %[4]s}
---
`, name, level, role, g)
		}
	}
	// Two stale objects, so the sweep has candidates: a binding the project no longer declares, and
	// one of a removed project whose namespace is still labeled.
	b.WriteString(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-legacy
  namespace: inventory
  labels: {autoshift.io/project: inventory, autoshift.io/managed-by: project-onboarding}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: view}
subjects:
  - {apiGroup: rbac.authorization.k8s.io, kind: Group, name: legacy-viewers}
---
apiVersion: v1
kind: Namespace
metadata:
  name: retired-app
  labels: {autoshift.io/project: retired-app, autoshift.io/managed-by: project-onboarding}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-admin
  namespace: retired-app
  labels: {autoshift.io/project: retired-app, autoshift.io/managed-by: project-onboarding}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: admin}
subjects:
  - {apiGroup: rbac.authorization.k8s.io, kind: Group, name: retired-admins}
---
`)
	// One object of every swept kind that project onboarding does not own, so the fake API server
	// serves the kind (lists of it return nothing owned).
	for _, k := range [][2]string{{"v1", "ResourceQuota"}, {"v1", "LimitRange"}, {"networking.k8s.io/v1", "NetworkPolicy"},
		{"k8s.ovn.org/v1", "EgressFirewall"}, {"policy.networking.k8s.io/v1alpha1", "AdminNetworkPolicy"}, {"k8s.ovn.org/v1", "EgressIP"}} {
		fmt.Fprintf(&b, "apiVersion: %s\nkind: %s\nmetadata:\n  name: unowned\n  namespace: unrelated\n---\n", k[0], k[1])
	}
	// The OVN-Kubernetes CRDs and the Network config of the hub, from the shared stubs.
	for _, f := range []string{"project-onboarding-network.yaml", "project-onboarding-anp.yaml",
		"project-onboarding-egressfirewall.yaml", "project-onboarding-egressip.yaml"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range splitYAMLDocuments(string(raw)) {
			if strings.Contains(doc, "kind: CustomResourceDefinition") || strings.Contains(doc, "kind: Network\n") {
				b.WriteString(strings.TrimSpace(doc) + "\n---\n")
			}
		}
	}
	b.WriteString(`apiVersion: v1
kind: ConfigMap
metadata:
  name: project-onboarding-status
  namespace: open-cluster-management-agent-addon
  labels: {autoshift.io/project-onboarding-status: "true"}
data:
  example: "an error from the single policy"
`)
	return b.String()
}

// migrationScenarios are the cluster states the golden file covers.
var migrationScenarios = []struct {
	name        string
	remediation string
	prune       bool
}{
	{"prune off", "enforce", false},
	{"prune on", "enforce", true},
	{"dry-run prune on", "inform", true},
}

// migrationSnapshot maps every object entry except the status ConfigMaps to its JSON (the whole
// object-templates-raw item, with the remediationAction of the ConfigurationPolicy that carries
// it), sorted; an entry rendered twice is listed twice.
func migrationSnapshot(t *testing.T, objs []onboardingObject) []string {
	t.Helper()
	var out []string
	for _, o := range objs {
		if isStatus(o) {
			continue
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(map[string]interface{}{"remediationAction": o.remediationAction, "item": o.item}); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.TrimSpace(buf.String()))
	}
	sort.Strings(out)
	return out
}

func migrationRun(t *testing.T, remediation string, prune bool) []onboardingObject {
	t.Helper()
	labels := map[string]string(nil)
	if prune {
		labels = pruneOn
	}
	r := stressRun(t, stressOpts{allShards: true, remediation: remediation, labels: labels, noTestdata: true,
		extra: migrationLive(t), mutate: migrationHubConfig(t)})
	for _, s := range stressPolicySizes(t, r.hub) {
		t.Logf("%s: %d bytes, estimate %d", s.name, s.real, s.est)
	}
	return r.objs
}

func TestProjectOnboarding_MigrationGolden(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repoRoot(t), migrationGoldenPath)
	got := map[string][]string{}
	for _, sc := range migrationScenarios {
		got[sc.name] = migrationSnapshot(t, migrationRun(t, sc.remediation, sc.prune))
	}
	if os.Getenv("ONBOARDING_MIGRATION_GOLDEN") == "write" {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string][]string
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	for _, sc := range migrationScenarios {
		w, g := want[sc.name], got[sc.name]
		if len(w) == 0 {
			t.Fatalf("%s: the golden file has no entries", sc.name)
		}
		wantSet := map[string]int{}
		for _, s := range w {
			wantSet[s]++
		}
		for _, s := range g {
			wantSet[s]--
		}
		for s, n := range wantSet {
			switch {
			case n > 0:
				t.Errorf("%s: the single policy rendered this, the shards do not:\n  %s", sc.name, s)
			case n < 0:
				t.Errorf("%s: the shards render this, the single policy did not:\n  %s", sc.name, s)
			}
		}
	}
}
