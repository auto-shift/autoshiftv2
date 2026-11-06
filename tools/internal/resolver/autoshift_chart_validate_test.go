//go:build integration

package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoShiftChart_RejectsAddonOutsideOpenShiftGitOps proves the chart refuses gitops-infra:
// 'addon' on a cluster whose gitops-namespace is not openshift-gitops, the namespace the Red Hat
// Advanced Cluster Management add-on always installs into. TestAutoShiftChart_ValuesProfiles only
// shows that clean profiles pass; without this a validator that never fires would pass too.
func TestAutoShiftChart_RejectsAddonOutsideOpenShiftGitOps(t *testing.T) {
	root := repoRoot(t)
	chartDir := filepath.Join(root, "autoshift")
	globalValues := filepath.Join(root, "autoshift", "values", "global.yaml")
	base := filepath.Join(root, "autoshift", "values", "clustersets", "_example.yaml")
	for _, p := range []string{chartDir, globalValues, base} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("required path missing, skipping: %s", p)
		}
	}

	cases := []struct {
		name    string
		overlay string
		want    string
	}{
		{
			name: "clusterset",
			overlay: `managedClusterSets:
  managed:
    labels:
      gitops-infra: 'addon'
      gitops-namespace: 'my-gitops'
`,
			want: "managedClusterSets['managed']",
		},
		{
			// The cluster sets only the mode; the namespace comes from its clusterset.
			name: "cluster-inherits-clusterset",
			overlay: `managedClusterSets:
  managed:
    labels:
      gitops-namespace: 'my-gitops'
clusters:
  edge1:
    config:
      clusterSet: 'managed'
    labels:
      gitops-infra: 'addon'
`,
			want: "clusters['edge1']",
		},
		{
			// No label at all: the namespace falls back to gitopsNamespace.
			name: "global-gitops-namespace",
			overlay: `gitopsNamespace: 'my-gitops'
managedClusterSets:
  managed:
    labels:
      gitops-infra: 'addon'
`,
			want: "managedClusterSets['managed']",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			overlay := filepath.Join(t.TempDir(), "overlay.yaml")
			if err := os.WriteFile(overlay, []byte(tc.overlay), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := HelmTemplate(chartDir, globalValues, base, overlay)
			if err == nil {
				t.Fatal("helm template succeeded; want the add-on validator to fail")
			}
			if !strings.Contains(err.Error(), "Infra Argo CD add-on validation failed") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an add-on validation error naming %s, got:\n%v", tc.want, err)
			}
		})
	}
}
