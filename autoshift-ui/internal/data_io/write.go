package data_io

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

func WriteHubValues(hubVals any) {

	yEdits, err := yaml.Marshal(hubVals)
	if err != nil {
		log.Println(err)
	}
	err = os.WriteFile("../../data/values.hub.yaml", yEdits, 0644)
	if err != nil {
		log.Println(err)
	}
}

func WriteToLogs() {

}
