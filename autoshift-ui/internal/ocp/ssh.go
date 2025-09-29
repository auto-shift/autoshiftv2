package ocp

import (
	"fmt"
	"os/exec"
	"strings"
)

func Ssh_bastion_login(domain, user, pass string) {
	cmd := exec.Command("/usr/local/bin/ssh", "-o ProxyCommand='oc login api."+domain+":6443' --username="+user+" --password="+pass)
	var out strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	fmt.Println("output string:")
	fmt.Println(out.String())
	err := cmd.Run()
	if err != nil {
		fmt.Println(stderr.String())
	} else {

		fmt.Println(out.String())
	}
}
