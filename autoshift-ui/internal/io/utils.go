package io

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type testStructs struct {
	AllTest map[string]string `yaml:"tests"`
}

func WriteConfigs() {

	ts := testStructs{}
	ts.AllTest = make(map[string]string)
	ts.AllTest["k1"] = "v1"
	ts.AllTest["k2"] = "v2"
	ts.AllTest["k3"] = "v3"
	ts.AllTest["k4"] = "v4"

	fmt.Println(ts)

	yEdits, err := yaml.Marshal(ts)
	if err != nil {
		log.Println(err)
	}
	err = os.WriteFile("../../data/values.hub.yaml", yEdits, 0644)
	if err != nil {
		log.Println(err)
	}
}

// CheckIfError should be used to naively panics if an error is not nil.
func CheckIfError(err error) {
	if err == nil {
		return
	}

	fmt.Printf("\x1b[31;1m%s\x1b[0m\n", fmt.Sprintf("error: %s", err))
	os.Exit(1)
}
