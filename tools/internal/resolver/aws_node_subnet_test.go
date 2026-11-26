//go:build integration

package resolver

import (
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

// TestAWSNodePolicies_ExistingMachineSet covers MachineSets the policies find already in place.
// The older policies copied only the worker's first security group, so an upgraded cluster has a
// subset of the worker's list: that must resolve, and musthave adds the rest. A security group
// the worker does not have, or a different subnet, must fail with a message naming the MachineSet.
func TestAWSNodePolicies_ExistingMachineSet(t *testing.T) {
	cases := []struct {
		name    string
		subnet  map[string]interface{}
		sgs     []interface{}
		wantErr string // empty: must resolve
	}{
		{"subset of the worker's security groups (upgrade)",
			map[string]interface{}{"id": "subnet-0fake0000000000a01"},
			[]interface{}{map[string]interface{}{"id": "sg-0fake0000000000a01"}}, ""},
		{"same security groups as the worker",
			map[string]interface{}{"id": "subnet-0fake0000000000a01"},
			[]interface{}{map[string]interface{}{"id": "sg-0fake0000000000a01"}, map[string]interface{}{"id": "sg-0fake0000000000a02"}}, ""},
		{"a security group the worker does not have",
			map[string]interface{}{"id": "subnet-0fake0000000000a01"},
			[]interface{}{map[string]interface{}{"id": "sg-0fake0000000000a01"}, map[string]interface{}{"id": "sg-0fake00000000000ff"}},
			"security groups the worker MachineSet"},
		{"a different subnet",
			map[string]interface{}{"filters": []interface{}{map[string]interface{}{"name": "tag:Name", "values": []interface{}{"test-cluster-abc12-subnet-private-us-east-2a"}}}},
			[]interface{}{map[string]interface{}{"id": "sg-0fake0000000000a01"}},
			"has a subnet that differs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var extra []unstructured.Unstructured
			for _, role := range []string{"infra", "storage"} {
				extra = append(extra, unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "machine.openshift.io/v1beta1",
					"kind":       "MachineSet",
					"metadata": map[string]interface{}{
						"name":      "test-cluster-abc12-" + role + "-us-east-2a",
						"namespace": "openshift-machine-api",
					},
					"spec": map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{
						"providerSpec": map[string]interface{}{"value": map[string]interface{}{
							"subnet": tc.subnet, "securityGroups": tc.sgs,
						}},
					}}},
				}})
			}
			results := runAWSNodePolicies(t, extra)
			for _, policy := range []string{"stable/infra-nodes", "stable/storage-nodes"} {
				res, ok := results[policy]
				if !ok {
					t.Fatalf("%s: no result", policy)
				}
				msgs := append(append([]string{}, res.ResolveWarns...), res.SpokeWarns...)
				if res.Err != nil {
					msgs = append(msgs, res.Err.Error())
				}
				all := strings.Join(msgs, "\n")
				if tc.wantErr == "" {
					if res.Err != nil || strings.Contains(all, "error calling fail") {
						t.Errorf("%s: should resolve, got: %s", policy, all)
					}
					continue
				}
				if !strings.Contains(all, tc.wantErr) {
					t.Errorf("%s: want a failure containing %q, got: %s", policy, tc.wantErr, all)
				}
			}
		})
	}
}

// runAWSNodePolicies runs the pipeline with the shared fixtures plus extra cluster resources and
// returns the infra-nodes and storage-nodes results by policy.
func runAWSNodePolicies(t *testing.T, extra []unstructured.Unstructured) map[string]ChartResult {
	t.Helper()
	root := repoRoot(t)
	policiesDir := filepath.Join(root, "policies")
	valuesDir := filepath.Join(root, "autoshift", "values")
	testdataDir := filepath.Join(root, "tools", "testdata")
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: BuildSyntheticLabels(declared)}
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
	seed := append(append(syntheticCMs, testResources...), extra...)
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
	out := map[string]ChartResult{}
	for _, res := range results {
		if res.Policy == "stable/infra-nodes" || res.Policy == "stable/storage-nodes" {
			out[res.Policy] = res
		}
	}
	return out
}
