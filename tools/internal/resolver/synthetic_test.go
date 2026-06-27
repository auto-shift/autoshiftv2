package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---- helpers ---------------------------------------------------------------

func mustWriteFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	return full
}

// hubExampleYAML is a minimal _example.yaml with labels and config.
const hubExampleYAML = `
hubClusterSets:
  hub:
    labels:
      cert-manager: 'true'
      cert-manager-channel: stable-v1
      disconnected-mirror: 'false'
    config:
      registry: registry.example.com
      registryNamespace: openshift4
`

// clusterInstallExampleYAML is a minimal _example-cluster-install.yaml.
const clusterInstallExampleYAML = `
clusters:
  example-cluster:
    config:
      baseDomain: example.com
      pullSecretRef: pull-secret
`

func makeFakeValuesDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "clustersets"), "_example.yaml", hubExampleYAML)
	mustWriteFile(t, filepath.Join(root, "clusters"), "_example-cluster-install.yaml", clusterInstallExampleYAML)
	return root
}

// ---- ExtractExampleConfigs -------------------------------------------------

func TestExtractExampleConfigs_LabelsExtracted(t *testing.T) {
	root := makeFakeValuesDir(t)
	cfg, err := ExtractExampleConfigs(root)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}

	for _, key := range []string{"cert-manager", "cert-manager-channel", "disconnected-mirror"} {
		if _, ok := cfg.BareLabels[key]; !ok {
			t.Errorf("BareLabels missing key %q", key)
		}
	}
	if cfg.BareLabels["cert-manager-channel"] != "stable-v1" {
		t.Errorf("cert-manager-channel = %q, want stable-v1", cfg.BareLabels["cert-manager-channel"])
	}
}

func TestExtractExampleConfigs_HubConfigExtracted(t *testing.T) {
	root := makeFakeValuesDir(t)
	cfg, err := ExtractExampleConfigs(root)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}

	if cfg.HubConfig == nil {
		t.Fatal("HubConfig is nil")
	}
	if cfg.HubConfig["registry"] != "registry.example.com" {
		t.Errorf("HubConfig[registry] = %v, want registry.example.com", cfg.HubConfig["registry"])
	}
}

func TestExtractExampleConfigs_ClusterInstallConfigExtracted(t *testing.T) {
	root := makeFakeValuesDir(t)
	cfg, err := ExtractExampleConfigs(root)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}

	if cfg.ClusterInstallConfig == nil {
		t.Fatal("ClusterInstallConfig is nil")
	}
	if cfg.ClusterInstallConfig["baseDomain"] != "example.com" {
		t.Errorf("ClusterInstallConfig[baseDomain] = %v, want example.com", cfg.ClusterInstallConfig["baseDomain"])
	}
}

func TestExtractExampleConfigs_MergesAllClusterInstallFiles(t *testing.T) {
	// Two cluster-install example files: one baremetal (has "hosts"), one AWS (has "aws").
	// Both must be merged so all platform config sections are present.
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "clustersets"), "_example.yaml", hubExampleYAML)
	mustWriteFile(t, filepath.Join(root, "clusters"), "_example-cluster-install.yaml", `
clusters:
  my-cluster:
    config:
      clusterSet: managed
      hosts:
        master-0:
          bmcIP: '192.168.1.10'
      clusterInstall:
        platform: baremetal
        baseDomain: baremetal.example.com
`)
	mustWriteFile(t, filepath.Join(root, "clusters"), "_example-cluster-install-aws.yaml", `
clusters:
  my-aws-cluster:
    config:
      clusterSet: managed
      aws:
        region: us-east-1
        credentialRef: aws-creds
      clusterInstall:
        platform: aws
        baseDomain: aws.example.com
`)

	cfg, err := ExtractExampleConfigs(root)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}

	if cfg.ClusterInstallConfig == nil {
		t.Fatal("ClusterInstallConfig is nil")
	}

	// "hosts" from baremetal example must be present.
	if _, ok := cfg.ClusterInstallConfig["hosts"]; !ok {
		t.Errorf("ClusterInstallConfig missing 'hosts' from baremetal example")
	}

	// "aws" from AWS example must also be present.
	if _, ok := cfg.ClusterInstallConfig["aws"]; !ok {
		t.Errorf("ClusterInstallConfig missing 'aws' from AWS example — multi-file merge broken")
	}

	// Both platforms' clusterInstall sections got merged (aws wins since it's last, or baremetal wins — either way key exists).
	if _, ok := cfg.ClusterInstallConfig["clusterInstall"]; !ok {
		t.Errorf("ClusterInstallConfig missing 'clusterInstall' key")
	}
}

func TestExtractExampleConfigs_MissingClustersDir_Partial(t *testing.T) {
	// Only hub example, no clusters dir — should return labels+config but no install config.
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "clustersets"), "_example.yaml", hubExampleYAML)
	// clusters/ dir intentionally absent

	cfg, err := ExtractExampleConfigs(root)
	// Missing clusters dir should be an error.
	if err == nil {
		// If implementation treats missing clusters/ as non-fatal, ClusterInstallConfig is nil.
		if cfg.ClusterInstallConfig != nil {
			t.Errorf("expected nil ClusterInstallConfig when clusters dir missing")
		}
	}
}

// ---- GenerateSyntheticConfigMaps -------------------------------------------

func TestGenerateSyntheticConfigMaps_Names(t *testing.T) {
	cfg := &ExampleConfigs{
		BareLabels: map[string]string{"cert-manager": "true"},
		HubConfig:  map[string]interface{}{"registry": "registry.example.com"},
		ClusterInstallConfig: map[string]interface{}{"baseDomain": "example.com"},
	}

	cms, err := GenerateSyntheticConfigMaps(cfg, "lint-cluster", "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}

	want := map[string]bool{
		"cluster-set-config.hub":             false,
		"cluster-set-config.managed":         false,
		"managed-cluster-config.lint-cluster": false,
		"lint-cluster.rendered-config":        false,
	}
	for _, cm := range cms {
		name, _, _ := unstructured.NestedString(cm.Object,"metadata", "name")
		if _, ok := want[name]; ok {
			want[name] = true
		} else {
			t.Errorf("unexpected ConfigMap name: %s", name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing expected ConfigMap: %s", name)
		}
	}
}

func TestGenerateSyntheticConfigMaps_Namespace(t *testing.T) {
	cfg := &ExampleConfigs{
		HubConfig: map[string]interface{}{"registry": "reg.example.com"},
	}
	cms, err := GenerateSyntheticConfigMaps(cfg, "lint-cluster", "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	for _, cm := range cms {
		ns, _, _ := unstructured.NestedString(cm.Object,"metadata", "namespace")
		if ns != "policies-autoshift" {
			name, _, _ := unstructured.NestedString(cm.Object,"metadata", "name")
			t.Errorf("CM %s has namespace %q, want policies-autoshift", name, ns)
		}
	}
}

func TestGenerateSyntheticConfigMaps_RenderedConfigContainsHubAndCluster(t *testing.T) {
	cfg := &ExampleConfigs{
		HubConfig:            map[string]interface{}{"registry": "reg.example.com"},
		ClusterInstallConfig: map[string]interface{}{"baseDomain": "example.com"},
	}
	cms, err := GenerateSyntheticConfigMaps(cfg, "lint-cluster", "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}

	var renderedConfig string
	for _, cm := range cms {
		name, _, _ := unstructured.NestedString(cm.Object,"metadata", "name")
		if name == "lint-cluster.rendered-config" {
			renderedConfig, _, _ = unstructured.NestedString(cm.Object,"data", "config")
			break
		}
	}
	if renderedConfig == "" {
		t.Fatal("lint-cluster.rendered-config not found or has empty data.config")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(renderedConfig), &parsed); err != nil {
		t.Fatalf("parse rendered-config: %v", err)
	}
	if parsed["registry"] != "reg.example.com" {
		t.Errorf("rendered-config missing registry from hub config, got: %v", parsed)
	}
	if parsed["baseDomain"] != "example.com" {
		t.Errorf("rendered-config missing baseDomain from cluster install config, got: %v", parsed)
	}
}

func TestGenerateSyntheticConfigMaps_NilConfig(t *testing.T) {
	cms, err := GenerateSyntheticConfigMaps(nil, "lint-cluster", "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps(nil): %v", err)
	}
	// nil config should return nil or empty slice without error.
	if len(cms) != 0 {
		t.Errorf("expected 0 CMs for nil config, got %d", len(cms))
	}
}

// ---- NewSpokeResolver + ResolveSpokeTemplates ------------------------------

func TestNewSpokeResolver_CreatesWithoutError(t *testing.T) {
	_, err := NewSpokeResolver(nil)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
}

func TestResolveSpokeTemplates_SprigFunctions(t *testing.T) {
	r, err := NewSpokeResolver(nil)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}

	// A minimal ConfigurationPolicy with a spoke-side template using Sprig.
	rawYAML := `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-spoke-test
  namespace: test-ns
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1beta1
        kind: ConfigurationPolicy
        metadata:
          name: spoke-config
        spec:
          object-templates-raw: |
            - complianceType: musthave
              objectDefinition:
                apiVersion: v1
                kind: ConfigMap
                metadata:
                  name: {{ "test" | upper }}
`

	result := r.ResolveSpokeTemplates(rawYAML)

	// Sprig `upper` should resolve.
	if !strings.Contains(result.Resolved, "TEST") {
		t.Errorf("expected 'TEST' (upper) in resolved output, got:\n%s", result.Resolved)
	}
	if strings.Contains(result.Resolved, `{{ "test" | upper }}`) {
		t.Errorf("unresolved spoke template remained in output:\n%s", result.Resolved)
	}
}

func TestResolveSpokeTemplates_LookupMissingReturnsEmpty(t *testing.T) {
	r, err := NewSpokeResolver(nil)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}

	// A template that looks up a resource that doesn't exist — should produce a
	// warning but not a hard error, and the document should be present in output.
	rawYAML := `---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: policy-spoke-lookup
  namespace: test-ns
spec:
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1beta1
        kind: ConfigurationPolicy
        metadata:
          name: spoke-lookup
        spec:
          object-templates-raw: |
            - complianceType: musthave
              objectDefinition:
                apiVersion: v1
                kind: ConfigMap
                metadata:
                  name: test
                data:
                  clusterDomain: {{ (lookup "config.openshift.io/v1" "DNS" "" "cluster").spec.baseDomain | default "cluster.local" }}
`

	result := r.ResolveSpokeTemplates(rawYAML)

	// Output should contain the document (possibly with the default value).
	if result.Resolved == "" {
		t.Error("expected non-empty Resolved even when lookup fails")
	}
}

func TestResolveSpokeTemplates_NoTemplates_PassThrough(t *testing.T) {
	r, err := NewSpokeResolver(nil)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}

	rawYAML := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: plain-configmap
data:
  key: value
`

	result := r.ResolveSpokeTemplates(rawYAML)
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors for plain YAML: %v", result.Errors)
	}
	if !strings.Contains(result.Resolved, "plain-configmap") {
		t.Errorf("plain ConfigMap should pass through unchanged, got:\n%s", result.Resolved)
	}
}

// ---- WriteTestValues -------------------------------------------------------

func TestWriteTestValues_IncludesAllLabels(t *testing.T) {
	root := makeFakeValuesDir(t)
	cfg, err := ExtractExampleConfigs(root)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}

	tmpDir := t.TempDir()
	path, err := WriteTestValues(tmpDir, "lint-cluster", cfg)
	if err != nil {
		t.Fatalf("WriteTestValues: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test values: %v", err)
	}
	content := string(data)

	// All extracted labels should appear somewhere in the values file.
	for key := range cfg.BareLabels {
		if !strings.Contains(content, key) {
			t.Errorf("test values missing label key %q", key)
		}
	}
}

func TestWriteTestValues_NilConfig_WritesMinimal(t *testing.T) {
	tmpDir := t.TempDir()
	path, err := WriteTestValues(tmpDir, "lint-cluster", nil)
	if err != nil {
		t.Fatalf("WriteTestValues(nil): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty values file for nil config")
	}
}
