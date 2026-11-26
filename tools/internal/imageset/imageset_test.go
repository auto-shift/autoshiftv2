// Package imageset tests scripts/generate-imageset-config.sh against a fixture catalog, offline.
//
// The generator reads version pins from the values files and gives each pinned channel a minVersion,
// so that oc-mirror copies the pinned CSVs and not only the channel head. These tests run the real
// script with --dependencies-file and a fixture catalog cache, so they need bash, jq and yq v4 but no
// registry access.
package imageset

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const catalogImage = "registry.redhat.io/redhat/redhat-operator-index:v4.22"

type channel struct {
	Name       string `yaml:"name"`
	MinVersion string `yaml:"minVersion"`
	MaxVersion string `yaml:"maxVersion"`
}

type pkg struct {
	Name     string    `yaml:"name"`
	Channels []channel `yaml:"channels"`
}

type imageSet struct {
	Mirror struct {
		Operators []struct {
			Catalog  string `yaml:"catalog"`
			Packages []pkg  `yaml:"packages"`
		} `yaml:"operators"`
	} `yaml:"mirror"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "scripts", "generate-imageset-config.sh")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("scripts/generate-imageset-config.sh not found above the working directory")
		}
		dir = parent
	}
}

// generate runs the script on the given testdata values files and returns its output, the parsed
// ImageSetConfiguration (nil when the script failed) and the exit error.
func generate(t *testing.T, extraArgs []string, values ...string) (string, *imageSet, error) {
	return generateFromCatalog(t, "catalog", extraArgs, values...)
}

// generateFromCatalog seeds the catalog cache from the named testdata directory, whose layout
// mirrors testdata/catalog, then runs the script like generate does.
func generateFromCatalog(t *testing.T, catalog string, extraArgs []string, values ...string) (string, *imageSet, error) {
	t.Helper()
	for _, tool := range []string{"bash", "jq", "yq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	root := repoRoot(t)
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}

	// The script keys its catalog cache by the md5 of the image name followed by a newline
	cache := t.TempDir()
	sum := md5.Sum([]byte(catalogImage + "\n"))
	configs := filepath.Join(cache, hex.EncodeToString(sum[:]), "configs")
	if err := os.CopyFS(configs, os.DirFS(filepath.Join(testdata, catalog))); err != nil {
		t.Fatal(err)
	}

	files := make([]string, len(values))
	for i, v := range values {
		files[i] = filepath.Join(testdata, v)
	}
	out := filepath.Join(t.TempDir(), "imageset.yaml")
	args := append([]string{filepath.Join(root, "scripts", "generate-imageset-config.sh"),
		strings.Join(files, ","),
		"--operators-only", "--openshift-version", "4.22",
		"--dependencies-file", filepath.Join(testdata, "dependencies.json"),
		"--output", out}, extraArgs...)
	cmd := exec.Command("bash", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CATALOG_CACHE_DIR="+cache)
	log, runErr := cmd.CombinedOutput()
	if runErr != nil {
		if _, err := os.Stat(out); err == nil {
			t.Errorf("the script failed but left %s behind", out)
		}
		return string(log), nil, runErr
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the generated imageset: %v\n%s", err, log)
	}
	var set imageSet
	if err := yaml.Unmarshal(data, &set); err != nil {
		t.Fatalf("the generated imageset is not YAML: %v\n%s", err, data)
	}
	return string(log), &set, nil
}

// channels maps "package/channel" to its entry in the redhat-operator-index catalog
func channels(t *testing.T, set *imageSet) map[string]channel {
	t.Helper()
	got := map[string]channel{}
	for _, op := range set.Mirror.Operators {
		if op.Catalog != catalogImage {
			continue
		}
		for _, p := range op.Packages {
			for _, c := range p.Channels {
				got[p.Name+"/"+c.Name] = c
			}
		}
	}
	return got
}

func TestVersionPinsSetMinVersion(t *testing.T) {
	log, set, err := generate(t, nil, "clusterset.yaml", "cluster.yaml")
	if err != nil {
		t.Fatalf("generation failed: %v\n%s", err, log)
	}
	got := channels(t, set)
	want := map[string]string{
		// config.quay.versions wins over the clusterset's quay-version label (3.18.2)
		"quay-operator/stable-3.18": "3.18.0",
		// the cluster's quay-version label, on the channel that carries it
		"quay-operator/stable-3.17": "3.17.1",
		// a CSV name that is not <package>.v<version>, read from the YAML catalog layout
		"nfd/stable": "4.22.0-202610010000",
	}
	for key, min := range want {
		c, ok := got[key]
		if !ok {
			t.Errorf("%s missing from the imageset; got %v", key, got)
			continue
		}
		if c.MinVersion != min {
			t.Errorf("%s minVersion = %q, want %q", key, c.MinVersion, min)
		}
		// No maxVersion, so the channel head stays mirrored for clusters that track the channel
		if c.MaxVersion != "" {
			t.Errorf("%s has maxVersion %q; the channel head would be dropped", key, c.MaxVersion)
		}
	}
	if strings.Contains(log, "quay-operator.v3.18.2") {
		t.Errorf("the quay-version label was used although config.quay.versions overrides it:\n%s", log)
	}
}

func TestUnpinnedChannelTracksHead(t *testing.T) {
	log, set, err := generate(t, nil, "cluster.yaml")
	if err != nil {
		t.Fatalf("generation failed: %v\n%s", err, log)
	}
	got := channels(t, set)
	if c := got["quay-operator/stable-3.17"]; c.MinVersion != "3.17.1" {
		t.Errorf("quay-operator/stable-3.17 minVersion = %q, want 3.17.1", c.MinVersion)
	}
	if _, ok := got["quay-operator/stable-3.18"]; ok {
		t.Errorf("stable-3.18 is not requested by cluster.yaml but was mirrored")
	}
}

// A truncated cached catalog must fail generation: the slurping read that resolves the pins
// yields nothing, and continuing would report success without the pinned minVersion.
func TestTruncatedCatalogFails(t *testing.T) {
	log, _, err := generateFromCatalog(t, "catalog-truncated", nil, "clusterset.yaml")
	if err == nil {
		t.Fatalf("a truncated cached catalog should fail generation:\n%s", log)
	}
	if !strings.Contains(log, "could not be parsed") {
		t.Errorf("the failure does not name the unreadable catalog:\n%s", log)
	}
}

// One channel pinning both a release and its rebuild: the rebuild is the lowest by
// semantic-version precedence, so minVersion names it and oc-mirror copies the rebuild too.
// sort -V would put the plain release first and drop the rebuild below the range.
func TestPinPrereleaseIsTheLowest(t *testing.T) {
	log, set, err := generate(t, nil, "cluster-suffix-pin.yaml")
	if err != nil {
		t.Fatalf("generation failed: %v\n%s", err, log)
	}
	if c := channels(t, set)["quay-operator/stable-3.18"]; c.MinVersion != "3.18.2-202611031655" {
		t.Errorf("quay-operator/stable-3.18 minVersion = %q, want 3.18.2-202611031655", c.MinVersion)
	}
}

func TestPinMissingFromCatalogFails(t *testing.T) {
	log, _, err := generate(t, nil, "clusterset.yaml", "cluster-bad-pin.yaml")
	if err == nil {
		t.Fatalf("a pin the catalog lacks should fail generation:\n%s", log)
	}
	if !strings.Contains(log, "quay-operator.v3.16.0") {
		t.Errorf("the failure does not name the missing CSV:\n%s", log)
	}
}

// --all-operators builds the release imageset from the _example*.yaml files, whose pins are examples.
// A stale one must not block a release, as a package missing from the catalog does not.
func TestPinMissingFromCatalogWarnsWithAllOperators(t *testing.T) {
	log, set, err := generate(t, []string{"--all-operators"}, "clusterset.yaml", "cluster-bad-pin.yaml")
	if err != nil {
		t.Fatalf("generation failed: %v\n%s", err, log)
	}
	if !strings.Contains(log, "quay-operator.v3.16.0") {
		t.Errorf("no warning names the missing CSV:\n%s", log)
	}
	got := channels(t, set)
	if c := got["quay-operator/stable-3.17"]; c.MinVersion != "" {
		t.Errorf("quay-operator/stable-3.17 minVersion = %q, want none: its only pin is not in the catalog", c.MinVersion)
	}
	if c := got["quay-operator/stable-3.18"]; c.MinVersion != "3.18.0" {
		t.Errorf("quay-operator/stable-3.18 minVersion = %q, want 3.18.0", c.MinVersion)
	}
}
