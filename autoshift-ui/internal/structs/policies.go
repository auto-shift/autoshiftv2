package structs

import (
	"log"
	"sync"
)

// Policy Struct
type Policy struct {
	Name        string `yaml:"name"`
	Policy_type string `yaml:"policy_type"`
	Desc        string `yaml:"desc"`
	Alias       string `yaml:"alias"`
	IsSelected  bool   `yaml:"install"`
}

func (p *Policy) UpdateIsSelected() {
	p.IsSelected = !p.IsSelected
}

// Policies Struct
type policies struct {
	HubPolicies     map[string][]Policy `yaml:"hub_policies"`
	ManagedPolicies []Policy            `yaml:"managed_policies"`
}

var (
	singleInstancePolicies *policies
	pOnce                  sync.Once
)

// Policies Singleton
func CreatePolicies() *policies {
	pOnce.Do(func() {
		log.Println("Creating Policies instance")
		singleInstancePolicies = &policies{}
	})
	return singleInstancePolicies
}

// Getters
func (p policies) GetHubPolicies() map[string][]Policy {
	return p.HubPolicies
}

func (p policies) GetManagedPolicies() []Policy {
	return p.ManagedPolicies
}

// Setters
func (p *policies) AddHubPolicies(name string, pol []Policy) {
	p.HubPolicies[name] = pol
}
func (p *policies) AddManagedPolicy(pol Policy) {
	p.ManagedPolicies = append(p.ManagedPolicies, pol)
}
func (p *policies) AddClusterSet(csName string) {
	p.HubPolicies[csName] = []Policy{}
}

// Utils
func (p *policies) InitHubPoliciesMap() {
	p.HubPolicies = make(map[string][]Policy)
}
