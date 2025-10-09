package data_io

import (
	"asui/internal/structs"
	"asui/internal/utils"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func init() {

}

var policies = structs.CreatePolicies()
var AppDir string

func ReadPolicies() {

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
}

func ReadPolicyList() []structs.Policy {
	pList := []structs.Policy{}

	absPath, err := filepath.Abs("../../data/policy_list.yaml")
	if err != nil {
		fmt.Printf("error: %v\n", err)
	} else {

		data, err3 := os.ReadFile(absPath)
		if err3 != nil {
			log.Fatalf("Error reading YAML file: %v", err3)
		}

		err = yaml.Unmarshal(data, &pList)
		if err != nil {
			log.Fatalf("Error unmarshaling YAML: %v", err)
		}
	}
	fmt.Println("pList:")
	fmt.Println(pList)
	return pList
}

func ReadPolicyLabels() map[string]map[string]string {

	AppDir = "../../data/policy_labels.yaml"

	labels := make(map[string]map[string]string)

	data, err := os.ReadFile(AppDir)

	utils.CheckIfError(err)

	err = yaml.Unmarshal(data, &labels)

	utils.CheckIfError(err)

	return labels
}
