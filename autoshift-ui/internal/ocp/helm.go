package ocp

import (
	"asui/internal/data_io"
	"fmt"
	"os/exec"
	"strings"
)

func HelmInstalGitops() {
	if IsLoggedIn() {

		// tmp, err := os.CreateTemp("", "helm-ocp.yaml")
		// if err != nil {
		// 	log.Panicln(err)
		// }
		// if _, err := tmp.Write(data_io.FetchOcpYaml()); err != nil {
		// 	log.Fatal(err)
		// }

		cmd := exec.Command("helm", "upgrade", "--install", "openshift-gitops", data_io.Tdir+"/openshift-gitops", "-f", data_io.Tdir+"/autoshiftv2/policies/values.yaml")
		var out strings.Builder
		var stderr strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(out)
		}
	}

}
