package structs

import (
	"fmt"
	"log"
	"sync"
)

type hubValuesStruct struct {
	GitRepo           string `yaml:"autoshiftGitRepo"`
	BranchTag         string `yaml:"autoshiftGitBranchTag"`
	SelfManagedHubSet string `yaml:"selfManagedHubSet"`
	HubClusterSets    struct {
		ClusterSets map[string]ClusterSetLabels
	} `yaml:"hubClusterSets"`
	ManagedClusterSets struct {
		Managed struct {
			Labels map[string]string
		} `yaml:"managed"`
	} `yaml:"managedClusterSets"`
}

// PolicyLabels Struct
// type PolicyLabels struct {
// 	Labels map[string]string
// }

var (
	singleInstance *hubValuesStruct
	once           sync.Once
)

// Struct Singleton
func CreateHubValues() *hubValuesStruct {
	once.Do(func() {
		log.Println("Creating HubValues instance")
		singleInstance = &hubValuesStruct{}
	})
	return singleInstance
}

func (hv hubValuesStruct) FormatHubValues() string {
	hubValuesTemplate := `
	autoshiftGitRepo: %s
	autoshiftGitBranchTag: %s
	selfManagedHubSet: %s
	hubClusterSets:
	hub:
		labels:
	`
	return fmt.Sprintf(hubValuesTemplate, hv.GitRepo, hv.BranchTag, hv.SelfManagedHubSet)
}

// func (hv hubValuesStruct) WriteHubValues() {
// 	yEdits, err := yaml.Marshal(hv)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	err = os.WriteFile("../../data/values.hub.yaml", yEdits, 0644)
// 	if err != nil {
// 		log.Println(err)
// 	}
// }

// Get Methods
func (hv hubValuesStruct) GetGitRepo() string {
	return hv.GitRepo
}
func (hv hubValuesStruct) GetBranchTag() string {
	return hv.BranchTag
}
func (hv hubValuesStruct) GetSMHubSet() string {
	return hv.SelfManagedHubSet
}
func (hv hubValuesStruct) GetHubClusterSets(csKey string) ClusterSetLabels {
	return hv.HubClusterSets.ClusterSets[csKey]
}

// func (hv hubValuesStruct) GetPolicyLabels() map[string]string {
// 	return hv.HubClusterSets.Hub.Labels.PolicyLabels
// }

// Set Methods
func (hv *hubValuesStruct) SetGitRepo(gitRepo string) {
	hv.GitRepo = gitRepo
}
func (hv *hubValuesStruct) SetBranchTag(branchTag string) {
	hv.BranchTag = branchTag
}
func (hv *hubValuesStruct) SetSMHubSet(selfManagedHubSet string) {
	hv.SelfManagedHubSet = selfManagedHubSet
}
func (hv *hubValuesStruct) SetIsSelfManaged(csKey string, csValue ClusterSetLabels) {
	hv.HubClusterSets.ClusterSets[csKey] = csValue
}

// func (hv *hubValuesStruct) SetPolicyLabels(policyLabels map[string]string) {
// 	hv.HubClusterSets.Hub.Labels.PolicyLabels = policyLabels
// }

// Update Labels
// func (hv *hubValuesStruct) UpdatePolicyLabels(plKey, plValue string) {
// 	var pl map[string]string = hv.HubClusterSets.Hub.Labels.PolicyLabels
// 	pl[plKey] = plValue
// }

func (hv *hubValuesStruct) InitClusterSets() {
	hv.HubClusterSets.ClusterSets = make(map[string]ClusterSetLabels)
}

func (hv *hubValuesStruct) InitManagedLabelsMap() {
	hv.ManagedClusterSets.Managed.Labels = make(map[string]string)
}

func (hv *hubValuesStruct) AddManagedLabel(labelKey, labelValue string) {
	hv.ManagedClusterSets.Managed.Labels[labelKey] = labelValue
}

func (hv *hubValuesStruct) removeManagedLabels(labels map[string]string) {
	for k, _ := range labels {
		delete(hv.ManagedClusterSets.Managed.Labels, k)
	}
}
