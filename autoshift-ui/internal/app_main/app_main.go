package app_main

import (
	"asui/internal/forms"
	"asui/internal/io"
	"asui/internal/oc"
	"asui/internal/structs"
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var HubValues structs.HubValuesStruct

func Home(win fyne.Window) fyne.CanvasObject {

	mainTabs := container.NewAppTabs(
		container.NewTabItem("Configs", forms.Configs(win)),
		container.NewTabItem("Policies", forms.Policies(win)),
	)
	mainTabs.SetTabLocation(container.TabLocationLeading)
	homeContainer := container.NewBorder(topBorder(), bottomBorder(), nil, nil, mainTabs)
	return homeContainer
}

func topBorder() fyne.CanvasObject {

	ocpFormContainer := container.NewGridWithColumns(8)

	if oc.IsLoggedIn() {
		ocpFormContainer.Add(widget.NewLabel("Logged In"))
	} else {
		ocpUserNameEntry := widget.NewEntry()

		ocpAccessTokenEntry := widget.NewEntry()
		ocpApiUrl := widget.NewEntry()
		ocpFormButton := widget.NewButton("Log In", func() {})
		ocpFormContainer.Objects = []fyne.CanvasObject{
			widget.NewLabel("Please log in: "),
			widget.NewLabel("username: "),
			ocpUserNameEntry,
			widget.NewLabel("access token: "),
			ocpAccessTokenEntry,
			widget.NewLabel("OCP API url: "),
			ocpApiUrl,
			ocpFormButton,
		}

		fmt.Println("min-size: ")
		fmt.Println(ocpFormContainer.MinSize())

	}

	return container.NewVBox(ocpFormContainer, canvas.NewLine(color.RGBA{R: 128, G: 128, B: 128, A: 255}))
}

func bottomBorder() fyne.CanvasObject {

	updateButton := widget.NewButton("Update", func() {
		io.WriteConfigs()
	})
	deployButton := widget.NewButton("Deploy", func() {})
	homeActions := container.NewHBox(layout.NewSpacer(), updateButton, deployButton)

	return container.NewVBox(canvas.NewLine(color.RGBA{R: 128, G: 128, B: 128, A: 255}), homeActions)
}
