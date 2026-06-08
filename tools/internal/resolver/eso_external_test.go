//go:build integration

package resolver

import (
	"encoding/base64"
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
	trustName          = "policy-eso-hub-bootstrap-trust"
	spokeBootstrapName = "policy-eso-hub-bootstrap"
)

// selfSigned does not require baseDomain: it defaults to autoshift.io so the minted client-cert CN
// carries a clear origin marker without forcing the operator to set one. With no baseDomain in the
// rendered config, the hub trust policy must still mint a per-cluster Certificate whose CN ends in
// .autoshift.io.
func TestHubBootstrap_SelfSignedDefaultBaseDomain(t *testing.T) {
	raw := renderESOChart(t)
	selected := selectPolicies(t, raw, map[string]bool{trustName: true})
	seed := seedFor(t, map[string]interface{}{
		"mode":      "selfSigned",
		"hubServer": "https://api.hub.example.com:6443",
		// deliberately NO baseDomain
	})
	out := resolveBoth(t, selected, seed)

	wantCN := "autoshift-eso-client.lint-cluster.autoshift.io"
	if !strings.Contains(out, "commonName: "+wantCN) {
		t.Errorf("selfSigned: expected minted cert CN to default baseDomain to autoshift.io (%q); not found in:\n%s", wantCN, out)
	}
}

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
// resolution error, returning the fully-resolved YAML. Uses the default lint-cluster context.
func resolveBoth(t *testing.T, rawYAML string, seed []unstructured.Unstructured) string {
	t.Helper()
	return resolveBothAs(t, rawYAML, seed, "lint-cluster")
}

// resolveBothAs is resolveBoth with an explicit managed-cluster name, so tests can exercise the
// per-cluster naming/truncation paths (which key off .ManagedClusterName) with a non-default name.
func resolveBothAs(t *testing.T, rawYAML string, seed []unstructured.Unstructured, clusterName string) string {
	t.Helper()
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	hub := r.ResolvePolicy(rawYAML, HubContext{ManagedClusterName: clusterName})
	if len(hub.Errors) > 0 {
		t.Fatalf("hub resolution errors: %v", hub.Errors)
	}
	spokeIn := stripStringDefaults(hub.Resolved)
	spoke := spokeR.ResolveSpokeTemplates(spokeIn, HubContext{ManagedClusterName: clusterName})
	if len(spoke.Errors) > 0 {
		t.Fatalf("spoke resolution errors: %v", spoke.Errors)
	}
	return spoke.Resolved
}

// renderedConfigCMNamed is renderedConfigCM for an arbitrary cluster name (the hub reads
// <clusterName>.rendered-config for cert settings keyed off .ManagedClusterName).
func renderedConfigCMNamed(t *testing.T, clusterName string, hubBootstrap map[string]interface{}) unstructured.Unstructured {
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
			"name":      clusterName + ".rendered-config",
			"namespace": "policies-autoshift",
		},
		"data": map[string]interface{}{"config": string(cfgYAML)},
	}}
}

// managedClusterFixture builds an autoshift-owned ManagedCluster (owning-namespace == the policy
// namespace) so the hub trust policy treats it as eligible and mints a per-cluster client cert.
func managedClusterFixture(name string) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "cluster.open-cluster-management.io/v1",
		"kind":       "ManagedCluster",
		"metadata": map[string]interface{}{
			"name":   name,
			"labels": map[string]interface{}{"autoshift.io/owning-namespace": "policies-autoshift"},
		},
		"spec": map[string]interface{}{
			"hubAcceptsClient": true,
			"managedClusterClientConfigs": []interface{}{
				map[string]interface{}{"url": "https://api." + name + ".example.com:6443"},
			},
		},
	}}
}

// tlsSecretFixture builds a kubernetes.io/tls Secret whose data values are already base64-encoded
// strings (the API representation), mirroring the testdata client-cert fixtures.
func tlsSecretFixture(name, namespace string, data map[string]interface{}) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "kubernetes.io/tls",
		"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
		"data":       data,
	}}
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

// With deriveHubUrl enabled and no explicit hubServer, the copy policy looks the URL up itself via a
// hub-template Infrastructure lookup; the store URL must come from Infrastructure.status.apiServerURL.
func TestHubBootstrap_DeriveHubUrl(t *testing.T) {
	raw := renderESOChart(t)
	selected := selectPolicies(t, raw, map[string]bool{spokeBootstrapName: true})
	seed := seedFor(t, map[string]interface{}{
		"mode":         "selfSigned",
		"deriveHubUrl": true,
		// deliberately NO hubServer — the policy must derive it from Infrastructure (testdata fixture)
		"externalSecrets": []interface{}{
			map[string]interface{}{"name": "app-secrets", "namespace": "app-secrets"},
		},
	})
	out := resolveBoth(t, selected, seed)

	wantURL := "url: https://api.test-cluster.test.example.com:6443"
	if !strings.Contains(out, wantURL) {
		t.Errorf("deriveHubUrl: expected store URL from Infrastructure.status.apiServerURL (%q); not found in:\n%s", wantURL, out)
	}
}

// Default (deriveHubUrl off) with no hubServer must fail loudly — the policy makes no hub lookup.
func TestHubBootstrap_HubServerRequiredWithoutDerive(t *testing.T) {
	raw := renderESOChart(t)
	selected := selectPolicies(t, raw, map[string]bool{spokeBootstrapName: true})
	seed := seedFor(t, map[string]interface{}{
		"mode": "selfSigned",
		// no hubServer, deriveHubUrl defaults false
		"externalSecrets": []interface{}{
			map[string]interface{}{"name": "app-secrets", "namespace": "app-secrets"},
		},
	})

	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	hub := r.ResolvePolicy(selected, HubContext{ManagedClusterName: "lint-cluster"})
	if len(hub.Errors) == 0 {
		t.Fatalf("expected a hub resolution error when hubServer is unset and deriveHubUrl is off, got none\n%s", hub.Resolved)
	}
	var sawMsg bool
	for _, e := range hub.Errors {
		if strings.Contains(e, "hub apiserver URL unresolved") {
			sawMsg = true
		}
	}
	if !sawMsg {
		t.Errorf("expected the unresolved-hubServer fail; got: %v", hub.Errors)
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

// A cluster name longer than the CN budget must be truncated to the SAME segment on both sides:
// the hub trust policy names the minted client secret <prefix>-client-<seg>, and the spoke copy
// policy must derive the identical name to find and copy that secret. We seed the hub-minted secret
// under ONLY the truncated name, so if the spoke derived anything else the copy lookup would miss and
// spoke resolution would fail — making a clean resolveBoth proof that both sides agree.
func TestHubBootstrap_LongClusterNameTruncation(t *testing.T) {
	raw := renderESOChart(t)
	selected := selectPolicies(t, raw, map[string]bool{
		trustName:          true,
		spokeBootstrapName: true,
	})

	// selfSigned defaults: prefix autoshift-eso-client, baseDomain autoshift.io. CN budget is the room
	// the prefix + baseDomain (+2 dots) leave under 63 — computed here so the test tracks the policy.
	const longName = "external-secrets-managed-cluster-alpha" // 38 chars > budget
	budget := 61 - len("autoshift-eso-client") - len("autoshift.io")
	seg := longName
	if len(longName) > budget {
		seg = longName[:budget]
	}
	if seg == longName {
		t.Fatalf("test cluster name %q (%d chars) is not longer than the CN budget %d — pick a longer name so truncation is actually exercised", longName, len(longName), budget)
	}
	wantSecret := "hub-bootstrap-client-" + seg
	fullSecret := "hub-bootstrap-client-" + longName
	wantCN := "autoshift-eso-client." + seg + ".autoshift.io"

	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	certData := b64("trunc-match-cert")
	clientSecretData := map[string]interface{}{
		"tls.crt": certData,
		"tls.key": b64("trunc-match-key"),
		"ca.crt":  b64("trunc-match-ca"),
	}

	testdata, err := LoadTestResources(filepath.Join(repoRoot(t), "tools", "testdata"))
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	seed := append([]unstructured.Unstructured{
		renderedConfigCMNamed(t, longName, map[string]interface{}{
			"mode":      "selfSigned",
			"hubServer": "https://api.hub.example.com:6443",
			"externalSecrets": []interface{}{
				map[string]interface{}{"name": "app-secrets", "namespace": "app-secrets"},
			},
		}),
		managedClusterFixture(longName),
		// hub-minted client secret seeded ONLY under the truncated name
		tlsSecretFixture(wantSecret, "policies-autoshift", clientSecretData),
	}, testdata...)

	out := resolveBothAs(t, selected, seed, longName)

	// Hub trust policy: the minted Certificate's CN and secretName use the truncated segment.
	if !strings.Contains(out, "commonName: "+wantCN) {
		t.Errorf("expected minted cert CN to use the truncated cluster segment (%q); not found in:\n%s", wantCN, out)
	}
	if !strings.Contains(out, "secretName: "+wantSecret) {
		t.Errorf("expected minted client secret name %q (truncated); not found in:\n%s", wantSecret, out)
	}
	// The raw, untruncated cluster name must never appear in a resource name.
	if strings.Contains(out, fullSecret) {
		t.Errorf("untruncated client secret name %q must not appear — the segment was not truncated", fullSecret)
	}
	// Spoke copy: resolution only succeeds if it derived the same truncated name and found the seeded
	// secret; the copied data proves the lookup hit (and didn't silently fall through to a default).
	if !strings.Contains(out, certData) {
		t.Errorf("expected the copied client cert data (%q) in the spoke output — the copy policy did not match the truncated secret name", certData)
	}
}
