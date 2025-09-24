package structs

type Policy struct {
	Name        string `yaml:"name"`
	Policy_type string `yaml:"policy_type"`
	Desc        string `yaml:"desc"`
}

type Policies struct {
	Policies []Policy `yaml:"policies"`
}
