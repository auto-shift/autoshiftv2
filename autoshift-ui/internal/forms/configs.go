package forms

import (
	"asui/internal/ocp"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func Configs() fyne.CanvasObject {
	configsGrid := container.NewGridWithRows(
		2,
		container.NewGridWithColumns(2, gitConfigs(), deploymentConfigs()),
		layout.NewSpacer(),
	)
	return configsGrid
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
	formCard := widget.NewCard("Remote Repository", "", container.NewVBox(gitForm, layout.NewSpacer(), gitSubmitBtn))

	return formCard
}

func deploymentConfigs() fyne.CanvasObject {

	ocpUserNameEntry := widget.NewEntry()
	ocpAccessTokenEntry := widget.NewEntry()
	ocpApiUrlEntry := widget.NewEntry()
	ocpFormButton := widget.NewButton("Test Connection", func() {
		ocp.Login(ocpUserNameEntry.Text, ocpAccessTokenEntry.Text, ocpApiUrlEntry.Text)
		if ocp.IsLoggedIn() {
			log.Println("Successful Connection")
		}
	})

	ocpFormContainer := container.New(
		layout.NewFormLayout(),
		widget.NewLabel("Username: "),
		ocpUserNameEntry,
		widget.NewLabel("Access token: "),
		ocpAccessTokenEntry,
		widget.NewLabel("OCP API url: "),
		ocpApiUrlEntry,
	)

	contentContainer := container.NewVBox(ocpFormContainer, layout.NewSpacer(), ocpFormButton)
	ocpCard := widget.NewCard(
		"OCP Login",
		"",
		contentContainer,
	)

	// deploymentConfigsGrid := container.NewGridWithColumns(2, ocpCard, layout.NewSpacer())
	// deploymentConfigsGrid := container.NewVBox(ocpFormContainer)
	return ocpCard

}
