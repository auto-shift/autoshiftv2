package forms

import (
	"asui/internal/data_io"

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
	// gitBranchEntry := widget.NewEntry()

	gitUserEntry.SetText(gitVars.User)
	gitPassEntry.SetText(gitVars.Token)
	gitRepoEntry.SetText(gitVars.Repo)
	// gitBranchEntry.SetPlaceHolder("test")

	gitUserLabel := widget.NewLabel("Git UserName:")
	gitPassLabel := widget.NewLabel("Git Password:")
	gitRepoLabel := widget.NewLabel("Git Repository URL:")
	// gitBranchLabel := widget.NewLabel("Git Revision:")

	gitSubmitBtn := widget.NewButton("Update", func() {
		data_io.GitCloneToTemp(gitUserEntry.Text, gitPassEntry.Text, gitRepoEntry.Text)
		// defer data_io.FetchOcpYaml()
		// 	CurrentGitRepo = gitRepoEntry.Text
		// 	CurrentGitBranch = gitBranchEntry.Text
		// 	log.Println("GitRepo: " + CurrentGitRepo + " GitBranch :" + CurrentGitBranch)

	})
	gitForm := container.New(layout.NewFormLayout(), gitRepoLabel, gitRepoEntry, gitUserLabel, gitUserEntry, gitPassLabel, gitPassEntry)
	formCard := widget.NewCard("Remote Repository", "", container.NewVBox(gitForm, layout.NewSpacer(), gitSubmitBtn))

	return formCard
}
