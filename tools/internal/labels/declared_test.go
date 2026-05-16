package labels

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestExtractDeclaredFromFile_NestedLabels(t *testing.T) {
	body := `
hubClusterSets:
  hub:
    labels:
      cert-manager: 'true'
      cert-manager-channel: stable
      cert-manager-source: redhat-operators
managedClusterSets:
  managed:
    labels:
      nmstate: 'true'
clusters:
  my-cluster:
    config: {}
    labels:
      override-flag: 'false'
`
	path := writeTemp(t, "values.yaml", body)

	decls, err := ExtractDeclaredFromFile(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	wantKeys := map[string]string{
		"cert-manager":         "hubClusterSets.hub.labels",
		"cert-manager-channel": "hubClusterSets.hub.labels",
		"cert-manager-source":  "hubClusterSets.hub.labels",
		"nmstate":              "managedClusterSets.managed.labels",
		"override-flag":        "clusters.my-cluster.labels",
	}
	if len(decls) != len(wantKeys) {
		t.Fatalf("want %d declarations, got %d: %+v", len(wantKeys), len(decls), decls)
	}
	for _, d := range decls {
		wantPath, ok := wantKeys[d.Key]
		if !ok {
			t.Errorf("unexpected key: %s", d.Key)
			continue
		}
		if d.Path != wantPath {
			t.Errorf("%s: path = %q, want %q", d.Key, d.Path, wantPath)
		}
	}
}

func TestExtractDeclaredFromFile_LabelsKeyIsLiteral_NotRecursive(t *testing.T) {
	body := `
hubClusterSets:
  hub:
    labels:
      weird-label: 'yes'
      labels: 'this is not a nested block'
`
	path := writeTemp(t, "values.yaml", body)
	decls, err := ExtractDeclaredFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(decls))
	for _, d := range decls {
		keys = append(keys, d.Key)
	}
	sort.Strings(keys)
	want := []string{"labels", "weird-label"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys: got %v, want %v", keys, want)
	}
}

func TestExtractDeclaredFromFile_CommentsAreIgnored(t *testing.T) {
	// Commented-out labels should NOT be captured. If a label needs to be
	// in the catalog, it must be an actual YAML key.
	body := `
hubClusterSets:
  hub:
    labels:
      gitops: 'true'
      # gitops-namespace: 'openshift-gitops'
      # gitops-cluster-ca-bundle: 'true'
`
	path := writeTemp(t, "values.yaml", body)
	decls, err := ExtractDeclaredFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 1 || decls[0].Key != "gitops" {
		t.Errorf("expected only 'gitops', got %+v", decls)
	}
}

func TestExtractDeclaredFromFile_NonMapLabelsIgnored(t *testing.T) {
	body := `
hubClusterSets:
  hub:
    labels: "not a map"
`
	path := writeTemp(t, "values.yaml", body)
	decls, err := ExtractDeclaredFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 0 {
		t.Errorf("want 0 decls, got %+v", decls)
	}
}

func TestExtractDeclaredFromTree_ExamplesOnlyByDefault(t *testing.T) {
	root := t.TempDir()
	mkfile := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mkfile("clustersets/hub.yaml", `
hubClusterSets:
  hub:
    labels:
      profile-only-label: 'true'
`)
	mkfile("clustersets/_example.yaml", `
hubClusterSets:
  hub:
    labels:
      example-label: 'false'
      shared-label: 'true'
`)

	m, err := ExtractDeclaredFromTree(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["profile-only-label"]; ok {
		t.Errorf("profile-only-label should be excluded by default")
	}
	if _, ok := m["example-label"]; !ok {
		t.Errorf("example-label should be present")
	}
	if !m["example-label"].InExamples() {
		t.Errorf("example-label should be marked as in-examples")
	}

	m, err = ExtractDeclaredFromTree(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["profile-only-label"]; !ok {
		t.Errorf("profile-only-label should be present when includeProfiles=true")
	}
	if m["profile-only-label"].InExamples() {
		t.Errorf("profile-only-label should NOT be marked as in-examples")
	}
}
