package forms

import (
	"asui/internal/data_io"
	"asui/internal/utils"
	"fmt"
	"io"
	"log"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func gitConfigs() *widget.Card {
	gitVars := testVars.GetGitVars()
	// fmt.Println(gitVars)
	gitUserEntry := widget.NewEntry()
	gitPassEntry := widget.NewEntry()
	gitRepoEntry := widget.NewEntry()
	gitBranchEntry := widget.NewEntry()

	gitUserEntry.SetText(gitVars.User)
	gitPassEntry.SetText(gitVars.Token)
	gitRepoEntry.SetText(gitVars.Repo)
	gitBranchEntry.SetPlaceHolder("test")

	gitUserLabel := widget.NewLabel("Git UserName:")
	gitPassLabel := widget.NewLabel("Git Password:")
	gitRepoLabel := widget.NewLabel("Git Repository URL:")
	gitBranchLabel := widget.NewLabel("Git Revision:")

	gitSubmitBtn := widget.NewButton("Update", func() {
		repo := data_io.GitCloneToMemory(gitUserEntry.Text, gitPassEntry.Text, gitRepoEntry.Text)
		// 	CurrentGitRepo = gitRepoEntry.Text
		// 	CurrentGitBranch = gitBranchEntry.Text
		// 	log.Println("GitRepo: " + CurrentGitRepo + " GitBranch :" + CurrentGitBranch)
		wt, err := repo.Worktree()
		utils.CheckIfError(err)
		filePath := "autoshift/values.hub.yaml"

		// Open the file from the worktree's filesystem
		file, err := wt.Filesystem.Open(filePath)
		if err != nil {
			log.Fatalf("Error opening file: %v", err)
		}
		defer file.Close()

		// Read the file content
		content, err := io.ReadAll(file)
		if err != nil {
			log.Fatalf("Error reading file: %v", err)
		}

		fmt.Printf("Content of %s:\n%s\n", filePath, content)

	})
	gitForm := container.New(layout.NewFormLayout(), gitRepoLabel, gitRepoEntry, gitBranchLabel, gitBranchEntry, gitUserLabel, gitUserEntry, gitPassLabel, gitPassEntry)
	formCard := widget.NewCard("Remote Repository", "", container.NewVBox(gitForm, layout.NewSpacer(), gitSubmitBtn))

	return formCard
}
