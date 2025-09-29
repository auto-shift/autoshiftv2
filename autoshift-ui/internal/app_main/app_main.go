package app_main

import (
	"asui/internal/forms"
	"asui/internal/impl"
	"asui/internal/structs"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func init() {
	HubValues.SetGitRepo("https://github.com/auto-shift/autoshiftv2.git")
	HubValues.SetBranchTag("main")
	HubValues.SetSMHubSet("hub")
	HubValues.HubClusterSets.Hub.Labels.SelfManaged = true
}

var HubValues = structs.CreateHubValues()

func Home(win fyne.Window) fyne.CanvasObject {

	// testText := container.NewVBox(widget.NewRichTextWithText(HubValues.FormatHubValues()))
	mainTabs := container.NewAppTabs(
		container.NewTabItem("Git Configs", forms.Configs()),
		container.NewTabItem("Policies", forms.Policies()),
		container.NewTabItem("Deployment", forms.DeploymentConfigs()),
	)

	mainTabs.SetTabLocation(container.TabLocationLeading)
	homeContainer := container.NewBorder(topBorder(), bottomBorder(), nil, nil, mainTabs)
	return homeContainer
}

func topBorder() fyne.CanvasObject {

	return container.NewVBox(canvas.NewLine(color.RGBA{R: 128, G: 128, B: 128, A: 255}))
}

func bottomBorder() fyne.CanvasObject {

	updateButton := widget.NewButton("Update", func() {
		// data_io.WriteConfigs()
		impl.UpdateLabels()
	})
	deployButton := widget.NewButton("Deploy", func() {})
	homeActions := container.NewHBox(layout.NewSpacer(), updateButton, deployButton)

	return container.NewVBox(canvas.NewLine(color.RGBA{R: 128, G: 128, B: 128, A: 255}), homeActions)
}
