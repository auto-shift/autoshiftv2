package structs

type ClusterSetLabels struct {
	Labels struct {
		IsSelfManaged bool              `yaml:"self-managed"`
		Labels        map[string]string `yaml:"labels"`
	} `yaml:"labels"`
}

// setters
func (csl *ClusterSetLabels) SetIsSelfManaged(smVal bool) {
	csl.Labels.IsSelfManaged = smVal
}

// getters
func (csl ClusterSetLabels) GetAllLabels() map[string]string {
	return csl.Labels.Labels
}

// utils
func (csl *ClusterSetLabels) initCSLabels() {
	csl.Labels.Labels = make(map[string]string)
}

func (csl *ClusterSetLabels) addLabel(lkey, lval string) {
	csl.Labels.Labels[lkey] = lval
}

func (csl *ClusterSetLabels) removeLabels(labels map[string]string) {
	for k, _ := range labels {
		delete(csl.Labels.Labels, k)
	}
}
