package data_io

import (
	"asui/internal/utils"
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func WriteHubValues(hubVals any) {
	// yEdits, err := yaml.Marshal(hubVals)
	// if err != nil {
	// 	log.Println(err)
	// }

	buf := bytes.Buffer{}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(&hubVals)
	utils.CheckIfError(err)
	err = os.WriteFile("../../data/values.hub.yaml", buf.Bytes(), 0644)
	utils.CheckIfError(err)
}

func WritePolicies(policies any) {
	buf := bytes.Buffer{}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(&policies)
	utils.CheckIfError(err)
	err = os.WriteFile("../../data/policies.yaml", buf.Bytes(), 0644)
	utils.CheckIfError(err)
	fmt.Println("Policies Updated")
	fmt.Println(policies)
}

func writeYaml() {

}
