package forms

import (
	"asui/internal/ocp"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func DeploymentConfigs() fyne.CanvasObject {

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

	contentContainer := container.NewVBox(ocpFormContainer, ocpFormButton)
	ocpCard := widget.NewCard(
		"OCP Login",
		"",
		contentContainer,
	)

	deploymentConfigsGrid := container.NewGridWithColumns(2, ocpCard, layout.NewSpacer())
	// deploymentConfigsGrid := container.NewVBox(ocpFormContainer)
	return deploymentConfigsGrid
}
