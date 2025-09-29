package impl

import (
	"asui/internal/data_io"
	"asui/internal/structs"
	"fmt"
)

func init() {
	policyLabels = data_io.ReadPolicyLabels("../../data/policy_labels.yaml")
	hubValues.InitMap()
}

var (
	policyLabels map[string]map[string]string
	policies     = structs.CreatePolicies()
	hubValues    = structs.CreateHubValues()
)

func UpdateLabels() {

	// var labels = make(map[string]string)

	for _, policy := range policies.Policies {
		if policy.IsSelected {
			getLabels(policy.Alias)
		}
	}
	data_io.WriteHubValues(hubValues)
	for k, v := range hubValues.GetPolicyLabels() {
		fmt.Printf("Key: %s, Value: %s\n", k, v)
	}

}

func getLabels(alias string) {
	// labels := make(map[string]string)
	for k, v := range policyLabels[alias] {
		hubValues.UpdatePolicyLabels(k, v)
	}

}
