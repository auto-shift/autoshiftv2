package structs

import (
	"log"
	"sync"
)

type GitVars struct {
	User   string `yaml:"user"`
	Token  string `yaml:"token"`
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
}

type OcpVars struct {
	User  string `yaml:"user"`
	Token string `yaml:"token"`
	Url   string `yaml:"url"`
}

type testValStruct struct {
	Git GitVars `yaml:"git"`
	Ocp OcpVars `yaml:"ocp"`
}

var (
	testValInstance *testValStruct
	test_once       sync.Once
)

// Struct Singleton
func CreateTestVals() *testValStruct {
	test_once.Do(func() {
		log.Println("Creating Test instance")
		testValInstance = &testValStruct{}
	})
	return testValInstance
}

func (tvs testValStruct) GetOcpVars() OcpVars {
	return tvs.Ocp
}

func (tvs testValStruct) GetGitVars() GitVars {
	return tvs.Git
}
