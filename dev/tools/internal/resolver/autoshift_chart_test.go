//go:build integration

package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoShiftChart_ValuesProfiles renders the top-level autoshift/ chart against every values
// profile in the repo.
//
// This is the only thing that executes autoshift/templates/_validate-*.tpl. TestPipeline_EndToEnd
// renders policies/ charts and consumes autoshift/values/ as data — it reads the label declarations
// and builds ConfigMaps from hubClusterSets.*.config — but it never renders the chart those values
// are actually written for. So a values file could name a key no schema recognises, or omit a
// required one, and every policy would still resolve cleanly against it: the policies read the keys
// they know about and are indifferent to the rest.
//
// That gap was real. _example.yaml carried disconnected.mirrorRegistry.port (no such field),
// catalogs[].image instead of imagePath + tag, hugepages.defaultHugepagesSize instead of
// defaultSize, and no CA — four violations the validator catches instantly and nothing ever ran it.
//
// The release name is the chart directory's base name, "autoshift" at 9 characters, which keeps the
// derived policy namespace inside the 20-character limit _validate-naming.tpl enforces.
func TestAutoShiftChart_ValuesProfiles(t *testing.T) {
	root := repoRoot(t)

	chartDir := filepath.Join(root, "autoshift")
	globalValues := filepath.Join(root, "autoshift", "values", "global.yaml")
	for _, p := range []string{chartDir, globalValues} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("required path missing, skipping: %s", p)
		}
	}

	clusterSets := globValues(t, filepath.Join(root, "autoshift", "values", "clustersets"))
	if len(clusterSets) == 0 {
		t.Fatal("no clusterset values files found — the chart would go unvalidated")
	}

	// A clusterset profile stands alone: global.yaml plus the profile is what an Application
	// actually renders in the simplest deployment.
	for _, vf := range clusterSets {
		t.Run("clusterset/"+filepath.Base(vf), func(t *testing.T) {
			renderChart(t, chartDir, globalValues, vf)
		})
	}

	// A per-cluster override is a delta layered on a clusterset, never a whole configuration, so it
	// is rendered on top of the reference profile. _example.yaml declares every option, which makes
	// it the strictest base to layer onto.
	base := filepath.Join(root, "autoshift", "values", "clustersets", "_example.yaml")
	if _, err := os.Stat(base); err != nil {
		t.Logf("no _example.yaml clusterset; skipping per-cluster overrides")
		return
	}
	for _, vf := range globValues(t, filepath.Join(root, "autoshift", "values", "clusters")) {
		t.Run("cluster/"+filepath.Base(vf), func(t *testing.T) {
			renderChart(t, chartDir, globalValues, base, vf)
		})
	}
}

// globValues returns the .yaml files in dir, sorted, or nothing if dir is absent.
func globValues(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	return matches
}

// renderChart fails the test if helm template errors or produces nothing.
//
// The empty-output check is not redundant: the chart's guards use `fail`, but a values file that
// disabled every generator would exit zero having emitted nothing, and an ApplicationSet that
// deploys no policies is a silent failure rather than a working minimal install.
func renderChart(t *testing.T, chartDir string, valuesFiles ...string) {
	t.Helper()

	out, err := HelmTemplate(chartDir, valuesFiles...)
	if err != nil {
		// The validator reports every violation at once, so print the lot rather than making the
		// author rerun for each in turn.
		t.Fatalf("helm template failed:\n%v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("rendered zero documents")
	}
}
