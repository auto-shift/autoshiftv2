package data_io

import (
	"asui/internal/utils"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	git "github.com/go-git/go-git/v5"
	http "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/storage/memory"
)

func init() {
	Tdir = utils.CreateTempDir()
	fmt.Println("Temp directory:")
	fmt.Println(Tdir)
}

// vars
var (
	GitRepo *git.Repository
	File    billy.File
	Tdir    string
	// package only
	storer *memory.Storage
	fs     billy.Filesystem
	once   sync.Once
)

// structs
type GitVars struct {
	GitDir  string `yaml:"gitDir"`
	GitUrl  string `yaml:"gitUrl"`
	GitUser string `yaml:"gitUser"`
}

// func GitCloneToMemory(gitUser, gitPass, gitUrl string) {
// 	storer = memory.NewStorage()
// 	fs = memfs.New()
// 	log.Println("Cloning git repository " + gitUrl + " ...")
// 	// Cloning a remote repository into memory
// 	var err error
// 	GitRepo, err = git.Clone(storer, fs, &git.CloneOptions{
// 		Auth: &http.BasicAuth{
// 			Username: gitUser, // yes, this can be anything except an empty string
// 			Password: gitPass,
// 		},
// 		URL: gitUrl,
// 	})
// 	utils.CheckIfError(err)

// }

func FetchOcpYaml() {
	log.Println("Parsing worktree...")
	wt, err := GitRepo.Worktree()
	utils.CheckIfError(err)

	if wt == nil {
		fmt.Println("no worktree present")
	} else {

		filePath := "policies/openshift-gitops/values.yaml"
		log.Println("opening file: " + filePath)
		// Open the file from the worktree's filesystem
		File, err = wt.Filesystem.Open(filePath)
		if err != nil {
			log.Fatalf("Error opening file: %v", err)
		}
		defer File.Close()

		// Read the file content
		content, err := io.ReadAll(File)
		if err != nil {
			log.Fatalf("Error reading file: %v", err)
		} else {
			//cont to be saved to temp file. Logged to prevent none use error.
			log.Println(content)
		}

	}

	// // ... retrieves the branch pointed by HEAD
	// ref, err := repo.Head()
	// utils.CheckIfError(err)

	// // ... retrieves the commit history
	// cIter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	// utils.CheckIfError(err)

	// // ... just iterates over the commits, printing it
	// err = cIter.ForEach(func(c *object.Commit) error {
	// 	fmt.Println(c)
	// 	return nil
	// })
	// utils.CheckIfError(err)

	// GitRepo = *repo
	// Or initializing a new repository in memory
	// repo, err := git.PlainInit(storer, fs, &git.PlainInitOptions{})
	// if err != nil {
	// 	// handle error
	// }

	// file, err := fs.Open("path/to/your/file.txt")
	// if err != nil {
	// 	// handle error
	// }
	// defer file.Close()

	// Read the file content
	// content, err := io.ReadAll(file)
	// ...
}

// Methods for interacting with a git repository
func GitClone(gitUser, gitPass, gitDir, gitUrl string) {
	// var resp []string

	path := gitDir + "/autoShift"

	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.Mkdir(path, 0775)
		// TODO: handle error
		fmt.Println(err)
	}

	r, err := git.PlainClone(path, false, &git.CloneOptions{
		// The intended use of a GitHub personal access token is in replace of your password
		// because access tokens can easily be revoked.
		// https://help.github.com/articles/creating-a-personal-access-token-for-the-command-line/
		Auth: &http.BasicAuth{
			Username: gitUser, // yes, this can be anything except an empty string
			Password: gitPass,
		},
		URL:      gitUrl,
		Progress: os.Stdout,
	})
	if err != nil {
		fmt.Println("err1")
		fmt.Println(err)
	}

	ref, err := r.Head()
	if err != nil {
		fmt.Println("err2")
		fmt.Println(err)
	}

	err = r.Storer.SetReference(ref)
	utils.CheckIfError(err)

	// ... retrieving the commit object
	commit, err := r.CommitObject(ref.Hash())
	if os.IsNotExist(err) {

		// resp[0] = "Clone Failed"
		// resp[1] = fmt.Sprintln(err)
		// return resp
		fmt.Println(err)
	}

	// resp[0] = "Clone Successful"
	// resp[1] = fmt.Sprintln(commit)
	// return resp
	fmt.Println(commit)

}

func GitCloneToTemp(gitUser, gitPass, gitUrl string) {

	r, err := git.PlainClone(Tdir, false, &git.CloneOptions{
		// The intended use of a GitHub personal access token is in replace of your password
		// because access tokens can easily be revoked.
		// https://help.github.com/articles/creating-a-personal-access-token-for-the-command-line/
		Auth: &http.BasicAuth{
			Username: gitUser, // yes, this can be anything except an empty string
			Password: gitPass,
		},
		URL:      gitUrl,
		Progress: os.Stdout,
	})
	if err != nil {
		fmt.Println("err1")
		fmt.Println(err)
	}

	ref, err := r.Head()
	if err != nil {
		fmt.Println("err2")
		fmt.Println(err)
	}

	err = r.Storer.SetReference(ref)
	utils.CheckIfError(err)

	// ... retrieving the commit object
	commit, err := r.CommitObject(ref.Hash())
	if os.IsNotExist(err) {

		// resp[0] = "Clone Failed"
		// resp[1] = fmt.Sprintln(err)
		// return resp
		fmt.Println(err)
	}

	// resp[0] = "Clone Successful"
	// resp[1] = fmt.Sprintln(commit)
	// return resp
	fmt.Println(commit)
	verifyRepo()
}

func verifyRepo() {
	cmd := exec.Command("ls", Tdir)
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

// func gitBranch() {

// }

// func gitCheckout() {

// }

// func GitPull(repo, branch string) {

// }

// func GitPush(repo, branch string) {

// }

// read git configs
func ReadGitConfigs() GitVars {

	yfile, err := os.ReadFile("../../configs/vars.yml")
	if err != nil {

		log.Fatal(err)
	}

	var gitVars GitVars

	err2 := yaml.Unmarshal(yfile, &gitVars)
	if err2 != nil {
		panic(err2)
	}
	return gitVars

}

func WriteGitConfigs(gitEdits GitVars) {
	yEdits, err := yaml.Marshal(gitEdits)
	if err != nil {
		log.Println(err)
	}
	os.WriteFile("../../configs/vars.yml", yEdits, 0644)

}

// CheckIfError should be used to naively panics if an error is not nil.
// func CheckIfError(err error) {
// 	if err == nil {
// 		return
// 	}

// 	fmt.Printf("\x1b[31;1m%s\x1b[0m\n", fmt.Sprintf("error: %s", err))
// 	os.Exit(1)
// }
