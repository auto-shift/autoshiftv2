package io

import (
	"asui/internal/structs"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func GetPolicies() structs.Policies {
	var policies structs.Policies

	absPath, err := filepath.Abs("../../data/policies.yaml")
	if err != nil {
		fmt.Printf("error: %v\n", err)
	} else {

		data, err3 := os.ReadFile(absPath)
		if err3 != nil {
			log.Fatalf("Error reading YAML file: %v", err3)
		}

		err = yaml.Unmarshal(data, &policies)
		if err != nil {
			log.Fatalf("Error unmarshaling YAML: %v", err)
		}

	}
	return policies

}
