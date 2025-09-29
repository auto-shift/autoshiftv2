package structs

import (
	"log"
	"sync"
)

// PolicyLabels Struct
// type PolicyLabels struct {
// 	configs []map[string]string
// }

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
	Policies []Policy `yaml:"policies"`
}

var (
	singleInstancePolicies *policies
	syncOnce               sync.Once
)

// Struct Singleton
func CreatePolicies() *policies {
	syncOnce.Do(func() {
		log.Println("Creating Policies instance")
		singleInstancePolicies = &policies{}
	})
	return singleInstancePolicies
}

func (ps *policies) UpdatePolicies() {

}
