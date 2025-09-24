package oc

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Login(user, pass, domain string) bool {
	// cmdStr := fmt.Sprintf("oc login -u %s -p %s https://api.%s:6443", user, pass, domain)
	// fmt.Println("login string:")
	// fmt.Println(cmdStr)
	cmd := exec.Command("/usr/local/bin/oc", "login", "api."+domain+":6443", "--username="+user, "--password="+pass)
	// cmd := exec.Command("/usr/bin/bash", cmdStr)
	var out strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	fmt.Println("output string:")
	fmt.Println(out.String())
	err := cmd.Run()
	if err != nil {
		fmt.Println(stderr.String())
		get_cluster_main()
		return false
	} else {
		fmt.Println(out.String())
		return true
	}

}

func Logout() {
	cmd := exec.Command("/usr/local/bin/oc", "logout")
	var out strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println(stderr.String())
	} else {

		fmt.Println(out.String())
	}
}

func get_cluster_main() {
	fmt.Println(Get_nodes())
}

func Get_nodes() string {
	fmt.Println(os.Stat("../../internal/templates/nodes.tmpl"))
	cmd := exec.Command("/usr/local/bin/oc", "get", "nodes", "-o", "go-template-file=../../internal/templates/nodes.tmpl")
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stderr.String()
	}
	return stdout.String()
}

func IsLoggedIn() bool {
	cmd := exec.Command("/usr/local/bin/oc", "status")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	fmt.Println("logged in status:")
	err := cmd.Run()
	if err != nil {
		fmt.Println(stderr.String())
	} else {
		fmt.Println(cmd.Stdout)
	}
	return err == nil
}
