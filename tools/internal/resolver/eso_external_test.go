//go:build integration

package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

// The chart's per-cluster trust modes (config.eso.hubBootstrap.mode) cannot be exercised by the
// main e2e pipeline because that pipeline renders against a single _example config (selfSigned).
// This test renders the ESO chart once, then resolves the two hub-bootstrap policies against a
// rendered-config seeded with each external mode, asserting the mode-specific output:
//   externalCA                  — hub wires clientCA from the external bundle + RBAC to the derived CN;
//                                 spoke mints its own cert via the user issuer with that same CN.
//   externalCAReuseServingCert  — hub wires clientCA + RBAC to the discovered apiserver host;
//                                 spoke copies its apiserver serving cert into the store secret.

const (
	esoChart           = "policies/stable/external-secrets-operator"
	trustExternalName  = "policy-eso-hub-bootstrap-trust-external"
	spokeBootstrapName = "policy-eso-hub-bootstrap"
)

// renderESOChart helm-templates the ESO chart with hubClusterSets enabled so the hub-only policy
// bodies render. Mode-specific behaviour comes from the seeded rendered-config, not from values.
func renderESOChart(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	tmp := t.TempDir()
	valuesPath := filepath.Join(tmp, "values.yaml")
	values := `
policy_namespace: policies-autoshift
autoshift:
  dryRun: false
  evaluationInterval:
    compliant: 10m
    noncompliant: 30s
hubClusterSets:
  hub:
    labels:
      self-managed: 'true'
managedClusterSets:
  managed:
    labels: {}
`
	if err := os.WriteFile(valuesPath, []byte(values), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}
	raw, err := HelmTemplate(filepath.Join(root, esoChart), valuesPath)
	if err != nil {
		t.Fatalf("helm template: %v", err)
	}
	return raw
}

// renderedConfigCM builds the lint-cluster.rendered-config ConfigMap carrying an eso.hubBootstrap
// config block, which the hub templates read to select the trust mode.
func renderedConfigCM(t *testing.T, hubBootstrap map[string]interface{}) unstructured.Unstructured {
	t.Helper()
	cfgYAML, err := sigsyaml.Marshal(map[string]interface{}{
		"eso": map[string]interface{}{"hubBootstrap": hubBootstrap},
	})
	if err != nil {
		t.Fatalf("marshal rendered-config: %v", err)
	}
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "lint-cluster.rendered-config",
			"namespace": "policies-autoshift",
		},
		"data": map[string]interface{}{"config": string(cfgYAML)},
	}}
}

// selectPolicies returns only the Policy documents whose metadata.name is in names, joined back
// into a multi-doc string. Isolates the two policies under test from the rest of the chart.
func selectPolicies(t *testing.T, raw string, names map[string]bool) string {
	t.Helper()
	var out []string
	for _, doc := range splitYAMLDocuments(raw) {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil {
			continue
		}
		if obj["kind"] != "Policy" {
			continue
		}
		meta, _ := obj["metadata"].(map[string]interface{})
		name, _ := meta["name"].(string)
		if names[name] {
			out = append(out, doc)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no Policy docs matched %v — chart did not render the expected policies", names)
	}
	return strings.Join(out, "\n---\n")
}

// resolveBoth runs the hub pass then the spoke pass (mirroring the e2e pipeline) and fails on any
// resolution error, returning the fully-resolved YAML.
func resolveBoth(t *testing.T, rawYAML string, seed []unstructured.Unstructured) string {
	t.Helper()
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	hub := r.ResolvePolicy(rawYAML, HubContext{ManagedClusterName: "lint-cluster"})
	if len(hub.Errors) > 0 {
		t.Fatalf("hub resolution errors: %v", hub.Errors)
	}
	spokeIn := stripStringDefaults(hub.Resolved)
	spoke := spokeR.ResolveSpokeTemplates(spokeIn, HubContext{ManagedClusterName: "lint-cluster"})
	if len(spoke.Errors) > 0 {
		t.Fatalf("spoke resolution errors: %v", spoke.Errors)
	}
	return spoke.Resolved
}

func seedFor(t *testing.T, hubBootstrap map[string]interface{}) []unstructured.Unstructured {
	t.Helper()
	testdata, err := LoadTestResources(filepath.Join(repoRoot(t), "tools", "testdata"))
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	// rendered-config override goes first; resolver keeps the last write, but the testdata has no
	// rendered-config of its own so order is not load-bearing here.
	return append([]unstructured.Unstructured{renderedConfigCM(t, hubBootstrap)}, testdata...)
}

func TestHubBootstrap_ExternalCA(t *testing.T) {
	raw := renderESOChart(t)
	selected := selectPolicies(t, raw, map[string]bool{
		trustExternalName:  true,
		spokeBootstrapName: true,
	})
	seed := seedFor(t, map[string]interface{}{
		"mode":         "externalCA",
		"hubServer":    "https://api.hub.example.com:6443",
		"baseDomain":   "eso.hub.example.com",
		"certCNPrefix": "autoshift-eso-client",
		"spokeIssuer": map[string]interface{}{
			"name": "shared-ca-issuer", "kind": "ClusterIssuer", "group": "cert-manager.io",
		},
		"externalClientCA": map[string]interface{}{
			"namespace": "openshift-config", "name": "external-shared-ca", "key": "ca-bundle.crt",
		},
		"externalSecrets": []interface{}{
			map[string]interface{}{"name": "app-secrets", "namespace": "app-secrets"},
		},
	})
	out := resolveBoth(t, selected, seed)

	wantCN := "autoshift-eso-client.lint-cluster.eso.hub.example.com"
	checks := []struct{ needle, why string }{
		{"kind: APIServer", "hub must wire the external CA into APIServer.spec.clientCA"},
		{"name: hub-bootstrap-client-ca", "clientCA ConfigMap must be referenced"},
		{"fakeexternalca", "the external CA bundle must be materialized into openshift-config"},
		{"name: " + wantCN, "hub RBAC RoleBinding subject must be the derived CN"},
		{"kind: Certificate", "spoke must mint its own client cert in externalCA mode"},
		{"commonName: " + wantCN, "spoke-minted cert CN must match the hub RBAC subject"},
		{"name: shared-ca-issuer", "spoke cert must use the user-provided issuer"},
		{"kind: ClusterSecretStore", "the bootstrap store must still be created"},
	}
	for _, c := range checks {
		if !strings.Contains(out, c.needle) {
			t.Errorf("externalCA: missing %q\n  reason: %s", c.needle, c.why)
		}
	}
	// In externalCA mode the spoke mints locally — it must NOT try to copy a hub-minted secret.
	if strings.Contains(out, "fake-hub-bootstrap-client") {
		t.Errorf("externalCA: spoke must not copy a hub-minted client cert (selfSigned-only behaviour)")
	}
}

// An unknown mode must fail loudly rather than silently no-op (no clientCA wired, no cert
// minted/copied, yet the store still created). Every mode-gated policy carries the guard, so the
// spoke pass must surface a resolution error naming the bad mode.
func TestHubBootstrap_InvalidMode(t *testing.T) {
	raw := renderESOChart(t)
	selected := selectPolicies(t, raw, map[string]bool{
		trustExternalName:  true,
		spokeBootstrapName: true,
	})
	seed := seedFor(t, map[string]interface{}{
		"mode":      "externalca", // valid-looking typo of externalCA
		"hubServer": "https://api.hub.example.com:6443",
		"externalSecrets": []interface{}{
			map[string]interface{}{"name": "app-secrets", "namespace": "app-secrets"},
		},
	})

	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	hub := r.ResolvePolicy(selected, HubContext{ManagedClusterName: "lint-cluster"})
	if len(hub.Errors) > 0 {
		t.Fatalf("hub resolution errors (mode guard is runtime, not hub): %v", hub.Errors)
	}
	spoke := spokeR.ResolveSpokeTemplates(stripStringDefaults(hub.Resolved), HubContext{ManagedClusterName: "lint-cluster"})
	if len(spoke.Errors) == 0 {
		t.Fatalf("invalidMode: expected a resolution error for an unknown mode, got none\n%s", spoke.Resolved)
	}
	var sawMode bool
	for _, e := range spoke.Errors {
		if strings.Contains(e, "externalca") && strings.Contains(e, "mode") {
			sawMode = true
		}
	}
	if !sawMode {
		t.Errorf("invalidMode: error did not name the bad mode value; got: %v", spoke.Errors)
	}
}

func TestHubBootstrap_ExternalCAReuseServingCert(t *testing.T) {
	raw := renderESOChart(t)
	selected := selectPolicies(t, raw, map[string]bool{
		trustExternalName:  true,
		spokeBootstrapName: true,
	})
	seed := seedFor(t, map[string]interface{}{
		"mode":      "externalCAReuseServingCert",
		"hubServer": "https://api.hub.example.com:6443",
		"externalClientCA": map[string]interface{}{
			"namespace": "openshift-config", "name": "external-shared-ca", "key": "ca-bundle.crt",
		},
		"externalSecrets": []interface{}{
			map[string]interface{}{"name": "app-secrets", "namespace": "app-secrets"},
		},
	})
	out := resolveBoth(t, selected, seed)

	// Identity discovered from the ManagedCluster's registered apiserver host (testdata fixture).
	wantHost := "api.test-cluster.test.example.com"
	checks := []struct{ needle, why string }{
		{"kind: APIServer", "hub must wire the external CA into APIServer.spec.clientCA"},
		{"fakeexternalca", "the external CA bundle must be materialized into openshift-config"},
		{"name: " + wantHost, "hub RBAC subject must be the discovered apiserver host"},
		{"type: kubernetes.io/tls", "spoke must copy the serving cert as a TLS secret"},
		{"name: hub-bootstrap-client", "the copied serving cert must land under the store's client secret name"},
		{"kind: ClusterSecretStore", "the bootstrap store must still be created"},
	}
	for _, c := range checks {
		if !strings.Contains(out, c.needle) {
			t.Errorf("reuseServingCert: missing %q\n  reason: %s", c.needle, c.why)
		}
	}
	// This mode mints nothing — there must be no cert-manager Certificate.
	if strings.Contains(out, "kind: Certificate") {
		t.Errorf("reuseServingCert: must not mint a Certificate (it reuses the existing serving cert)")
	}
}
