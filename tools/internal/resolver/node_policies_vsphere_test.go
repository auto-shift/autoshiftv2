//go:build integration

package resolver

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	"gopkg.in/yaml.v3"
)

// awsOnlyProviderFields are the AWSMachineProviderConfig fields that a vSphere
// providerSpec does not have. userDataSecret and credentialsSecret are shared by
// both providers and are kept.
var awsOnlyProviderFields = []string{
	"ami", "placement", "instanceType", "iamInstanceProfile", "blockDevices",
	"securityGroups", "subnet", "tags", "deviceIndex",
}

// nodePoliciesPendingProviderZoneFix are provider-agnostic policies that still read
// the AWS-only placement.availabilityZone to find the worker's zone, so on a vSphere
// cluster they render a MachineSet named <infra-id>-<role>-<no value>. #309 fixes all
// four. The list is checked both ways: a policy that starts passing must be removed
// from it, so the fix and this test cannot silently drift apart.
var nodePoliciesPendingProviderZoneFix = map[string]bool{
	"policy-infra-nodes-test":   true,
	"policy-remove-infra-nodes": true,
	"policy-storage-nodes-test": true,
	"policy-worker-nodes-test":  true,
}

var policyNameInMessage = regexp.MustCompile(`policy-[a-z0-9-]+`)

// TestNodePolicies_VSphereOnlyWorkers renders the node policies against worker
// MachineSets that carry only vSphere fields, with no <role>-nodes-zone-N labels.
//
// The shared fixture in tools/testdata gives every worker MachineSet both AWS and
// vSphere fields, and the example values set a zone label for each role. Either one
// alone hides a policy that reads an AWS-only field: the zone comes from the label,
// or the AWS field is simply there. RunPipeline reloads fixtures from disk, so the
// vSphere-only variant is staged as a modified copy of tools/testdata rather than
// patched in memory.
//
// AWS-only policies (name containing "-aws") are skipped: their placements select on
// the provider label, so they never land on a vSphere cluster. Every other policy in
// the node charts must resolve with no error and no <no value>.
func TestNodePolicies_VSphereOnlyWorkers(t *testing.T) {
	root := repoRoot(t)
	testdataDir := t.TempDir()
	if err := copyTree(filepath.Join(root, "tools", "testdata"), testdataDir); err != nil {
		t.Fatalf("copy testdata: %v", err)
	}
	rewriteWorkersAsVSphereOnly(t, filepath.Join(testdataDir, "cluster-openshift-resources.yaml"))

	valuesDir := filepath.Join(root, "autoshift", "values")
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: BuildSyntheticLabels(declared),
	}
	for k := range ctx.ManagedClusterLabels {
		if strings.Contains(k, "-nodes-zone") {
			delete(ctx.ManagedClusterLabels, k)
		}
	}
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	cms, err := GenerateSyntheticConfigMaps(configs, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	stubs, err := LoadTestResources(testdataDir)
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	seed := append(cms, stubs...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	_, results, err := RunPipeline(filepath.Join(root, "policies"), ctx, nil, r, spokeR, declared, configs, testdataDir)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	charts := map[string]bool{"stable/infra-nodes": true, "stable/storage-nodes": true, "stable/worker-nodes": true}
	problems := map[string][]string{}
	seen := 0
	for _, res := range results {
		if !charts[res.Policy] {
			continue
		}
		seen++
		if res.Err != nil {
			t.Fatalf("%s: render failed: %v", res.Policy, res.Err)
		}
		msgs := append(append(append([]string{}, res.ResolveWarns...), res.SpokeWarns...), res.YAMLErrors...)
		for _, m := range msgs {
			name := policyNameInMessage.FindString(m)
			if name == "" || strings.Contains(name, "-aws") {
				continue
			}
			problems[name] = append(problems[name], m)
		}
	}
	if seen != len(charts) {
		t.Fatalf("expected %d node charts in the pipeline results, found %d", len(charts), seen)
	}

	var names []string
	for n := range problems {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if nodePoliciesPendingProviderZoneFix[n] {
			t.Logf("known (#309): %s does not resolve with vSphere-only workers", n)
			continue
		}
		first := problems[n][0]
		if len(first) > 300 {
			first = first[:300] + "..."
		}
		t.Errorf("%s does not resolve with vSphere-only workers (%d problems), first: %s", n, len(problems[n]), first)
	}
	for n := range nodePoliciesPendingProviderZoneFix {
		if _, still := problems[n]; !still {
			t.Errorf("%s now resolves with vSphere-only workers: remove it from nodePoliciesPendingProviderZoneFix", n)
		}
	}
}

// rewriteWorkersAsVSphereOnly strips the AWS-only providerSpec fields from every
// worker MachineSet in a staged fixture file. Comments in the staged copy are lost,
// which does not matter for a temporary file.
func rewriteWorkersAsVSphereOnly(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	rewritten := 0
	for {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if doc == nil {
			continue
		}
		name, _ := nested(doc, "metadata", "name").(string)
		if doc["kind"] == "MachineSet" && strings.Contains(name, "-worker-") {
			pv, ok := nested(doc, "spec", "template", "spec", "providerSpec", "value").(map[string]any)
			if !ok {
				t.Fatalf("%s: worker MachineSet %s has no providerSpec.value", path, name)
			}
			for _, k := range awsOnlyProviderFields {
				delete(pv, k)
			}
			pv["kind"] = "VSphereMachineProviderSpec"
			rewritten++
		}
		if err := enc.Encode(doc); err != nil {
			t.Fatalf("encode %s: %v", path, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if rewritten == 0 {
		t.Fatalf("%s: no worker MachineSet found to rewrite", path)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
