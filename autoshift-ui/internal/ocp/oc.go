package ocp

import (
	"asui/internal/utils"
	"bufio"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2/data/binding"
)

var (
	// TestLogs = []string{}
	BLogs    = binding.NewStringList()
	logger   = log.New(&utils.LogBuffer, "ocp: ", log.Ldate|log.Ltime)
	testBool = false
	ticker   = time.NewTicker(5 * time.Second)
)

func Login(user, pass, domain string) bool {
	// cmdStr := fmt.Sprintf("oc login -u %s -p %s https://api.%s:6443", user, pass, domain)
	// fmt.Println("login string:")
	// fmt.Println(cmdStr)

	cmd := exec.Command("oc", "login", domain, "--username="+user, "--password="+pass)

	var out strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		for _, e := range strings.Split(stderr.String(), "\n") {
			if e != "" {
				BLogs.Append(e)
			}
		}
		// get_cluster_main()
		// fmt.Println("err: " + err.Error())
		return false
	} else {
		for _, o := range strings.Split(out.String(), "\n") {
			if o != "" {
				BLogs.Append(o)
			}
		}
		return true
	}

}

func Logout() bool {
	cmd := exec.Command("/usr/local/bin/oc", "logout")
	var out strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		for _, e := range strings.Split(stderr.String(), "\n") {
			if e != "" {
				BLogs.Append(e)
			}
		}
		// get_cluster_main()
		// fmt.Println("err: " + err.Error())
		return false
	} else {
		for _, o := range strings.Split(out.String(), "\n") {
			if o != "" {
				BLogs.Append(o)
			}
		}
		return true
	}
}

func InstallPreReqs() {
	cmd := exec.Command("/bin/bash", "-c", "../../scripts/install-bootstrap.sh")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Println(err)
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		log.Println(err)
	}

	go func() {
		if err := cmd.Start(); err != nil {
			log.Println(err)
		}
	}()

	go func() {
		reader := bufio.NewReader(pipe)
		line, err := reader.ReadString('\n')
		for err == nil {
			log.Println(line)
			line, err = reader.ReadString('\n')
		}
	}()

	go func() {
		reader := bufio.NewReader(errPipe)
		line, err := reader.ReadString('\n')
		for err == nil {
			log.Println(line)
			line, err = reader.ReadString('\n')
		}
	}()
}

func InstallAutoShift() {
	cmd := exec.Command("/bin/bash", "-c", "../../scripts/install-autoshift.sh")
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

// checks if the multiclusterhub is running
func CheckMch() string {
	cmd := exec.Command("/usr/local/bin/oc", "get", "mch", "multiclusterhub", "-n", "open-cluster-management", "-o", "jsonpath='{.status.phase}'")
	var out strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		return "error"
	}
	fmt.Println(out)
	return out.String()
}

// func Get_nodes() string {
// 	log.Println(os.Stat("../../internal/templates/nodes.tmpl"))
// 	cmd := exec.Command("/usr/local/bin/oc", "get", "nodes", "-o", "go-template-file=../../internal/templates/nodes.tmpl")
// 	var stdout strings.Builder
// 	var stderr strings.Builder
// 	cmd.Stdout = &stdout
// 	cmd.Stderr = &stderr
// 	err := cmd.Run()
// 	if err != nil {
// 		return stderr.String()
// 	}
// 	return stdout.String()
// }

func IsLoggedIn() bool {
	cmd := exec.Command("oc", "status")
	var stderr strings.Builder
	var stdout strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	logger.Println("logged in status:")
	err := cmd.Run()
	if err != nil {
		for _, e := range strings.Split(stderr.String(), "\n") {
			if e != "" {
				BLogs.Append(e)
			}
		}
		// get_cluster_main()
		// fmt.Println("err: " + err.Error())
		return false
	} else {
		for _, o := range strings.Split(stdout.String(), "\n") {
			if o != "" {
				BLogs.Append(o)
			}
		}
		return true
	}
}

func TestLogsOutput() {
	if !testBool {
		testBool = true

		go func() {
			for range ticker.C {
				// This code will be executed every 5 seconds.
				BLogs.Append("the time is now: " + time.DateTime)
			}
		}()

		time.Sleep(20 * time.Second) // Run for 20 seconds
		ticker.Stop()                // Stop the ticker to release resources
		fmt.Println()
		fmt.Println("Ticker stopped. Exiting.")

		testBool = false
	} else {
		BLogs.Append("Logs already running")
	}
}
