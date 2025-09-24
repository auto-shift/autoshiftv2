package structs

type HubValuesStruct struct {
	GitRepo          string `yaml:"autoshiftGitRepo"`
	BranchTag        string `yaml:"autoshiftGitBranchTag"`
	SelfManageHubSet string `yaml:"selfManagedHubSet"`
	HubClusterSets   struct {
		Hub struct {
			Labels struct {
				SelfManaged bool `yaml:"self-managed"`
				Labels      map[string]string
			} `yaml:"labels"`
		} `yaml:"hub"`
	} `yaml:"hubClusterSets"`
}
