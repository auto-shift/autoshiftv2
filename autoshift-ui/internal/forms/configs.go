package forms

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func Configs() fyne.CanvasObject {
	// spacerGrid := container.NewGridWrap(fyne.NewSize(200, 100))

	return gitConfigs()
}

func gitConfigs() *widget.Card {

	gitUserEntry := widget.NewEntry()
	gitUserEntry.SetPlaceHolder("test")
	gitPassEntry := widget.NewEntry()
	gitPassEntry.SetPlaceHolder("test")
	gitRepoEntry := widget.NewEntry()
	gitRepoEntry.SetPlaceHolder("test")
	gitBranchEntry := widget.NewEntry()
	gitBranchEntry.SetPlaceHolder("test")

	gitUserLabel := widget.NewLabel("Git UserName:")
	gitPassLabel := widget.NewLabel("Git Password:")
	gitRepoLabel := widget.NewLabel("Git Repository URL:")
	gitBranchLabel := widget.NewLabel("Git Revision:")

	gitSubmitBtn := widget.NewButton("Update", func() {
		// 	CurrentGitRepo = gitRepoEntry.Text
		// 	CurrentGitBranch = gitBranchEntry.Text
		// 	log.Println("GitRepo: " + CurrentGitRepo + " GitBranch :" + CurrentGitBranch)
	})
	gitForm := container.New(layout.NewFormLayout(), gitRepoLabel, gitRepoEntry, gitBranchLabel, gitBranchEntry, gitUserLabel, gitUserEntry, gitPassLabel, gitPassEntry)
	formCard := widget.NewCard("Remote Repository", "", container.NewVBox(gitForm, gitSubmitBtn))

	return formCard
}
