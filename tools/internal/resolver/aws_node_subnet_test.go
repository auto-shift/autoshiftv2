//go:build integration

package resolver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// TestAWSNodePolicies_CopyWorkerSubnet guards the fix for #282. The AWS infra and storage
// node policies used to build the MachineSet's subnet as a tag:Name filter from the
// infrastructure ID (<infra-id>-subnet-private-<zone>). On a cluster installed into a
// pre-existing VPC the worker references its subnet by ID and the subnets carry unrelated
// Name tags, so the reconstructed filter matched nothing and no machine provisioned, while
// the policy reported Compliant.
//
// The us-east-2a worker in testdata has that pre-existing VPC shape (subnet and security
// group by raw ID). The policies must copy those values, not reconstruct them. Rendering
// alone cannot catch this: the reconstructed form is valid YAML, so only an assertion on
// the values does.
func TestAWSNodePolicies_CopyWorkerSubnet(t *testing.T) {
	root := repoRoot(t)
	policiesDir := filepath.Join(root, "policies")
	valuesDir := filepath.Join(root, "autoshift", "values")
	testdataDir := filepath.Join(root, "tools", "testdata")

	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: BuildSyntheticLabels(declared),
	}
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	syntheticCMs, err := GenerateSyntheticConfigMaps(configs, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	testResources, err := LoadTestResources(testdataDir)
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	seed := append(syntheticCMs, testResources...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	_, results, err := RunPipeline(policiesDir, ctx, nil, r, spokeR, declared, configs, testdataDir)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	for _, policy := range []string{"stable/infra-nodes", "stable/storage-nodes"} {
		t.Run(policy, func(t *testing.T) {
			var resolved string
			for _, res := range results {
				if res.Policy == policy {
					if res.Err != nil {
						t.Fatalf("%s did not resolve: %v", policy, res.Err)
					}
					resolved = res.ResolvedYAML
				}
			}
			if resolved == "" {
				t.Fatalf("%s: no resolved output", policy)
			}
			for _, want := range []string{"subnet-0fake0000000000a01", "sg-0fake0000000000a01"} {
				if !strings.Contains(resolved, want) {
					t.Errorf("%s: %q missing; the subnet or security group was not copied from the us-east-2a worker", policy, want)
				}
			}
			if strings.Contains(resolved, "test-cluster-abc12-subnet-private-us-east-2a") {
				t.Errorf("%s: subnet reconstructed from the infrastructure ID instead of copied from the worker", policy)
			}
		})
	}
}
