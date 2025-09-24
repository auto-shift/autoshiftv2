package forms

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func init() {
	CurrentGitRepo = "Test Repo"
	CurrentGitBranch = "Test Branch"
}

var CurrentGitRepo string
var CurrentGitBranch string
var CurrentGitUser string
var CurrentGitPass string

func Configs(win fyne.Window) fyne.CanvasObject {
	// spacerGrid := container.NewGridWrap(fyne.NewSize(200, 100))
	remoteRepoCard := widget.NewCard("Remote Repository", "", gitConfigs())
	cardGrids := container.New(layout.NewGridLayout(2), layout.NewSpacer(), remoteRepoCard)
	return cardGrids
}

func gitConfigs() fyne.CanvasObject {

	gitUserEntry := widget.NewEntry()
	gitUserEntry.SetPlaceHolder(CurrentGitUser)
	gitPassEntry := widget.NewEntry()
	gitPassEntry.SetPlaceHolder(CurrentGitRepo)
	gitRepoEntry := widget.NewEntry()
	gitRepoEntry.SetPlaceHolder(CurrentGitRepo)
	gitBranchEntry := widget.NewEntry()
	gitBranchEntry.SetPlaceHolder(CurrentGitBranch)

	gitUserLabel := widget.NewLabel("Git UserName:")
	gitPassLabel := widget.NewLabel("Git Password:")
	gitRepoLabel := widget.NewLabel("Git Repository URL:")
	gitBranchLabel := widget.NewLabel("Git Revision:")

	gitSubmitBtn := widget.NewButton("Update", func() {
		CurrentGitRepo = gitRepoEntry.Text
		CurrentGitBranch = gitBranchEntry.Text
		log.Println("GitRepo: " + CurrentGitRepo + " GitBranch :" + CurrentGitBranch)
	})
	gitForm := container.New(layout.NewFormLayout(), gitRepoLabel, gitRepoEntry, gitBranchLabel, gitBranchEntry, gitUserLabel, gitUserEntry, gitPassLabel, gitPassEntry)
	return container.NewVBox(gitForm, gitSubmitBtn)
}
