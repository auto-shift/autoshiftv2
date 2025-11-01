package forms

import (
	"asui/internal/ocp"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var (
	ocpFormButton       *widget.Button
	ocpUserNameEntry    *widget.Entry
	ocpAccessTokenEntry *widget.Entry
	ocpApiUrlEntry      *widget.Entry
)

func deploymentConfigs() fyne.CanvasObject {
	ocpVars := testVars.GetOcpVars()

	ocpFormButton = widget.NewButton("", func() {})
	ocpUserNameEntry = widget.NewEntry()
	ocpAccessTokenEntry = widget.NewEntry()
	ocpApiUrlEntry = widget.NewEntry()

	ocpUserNameEntry.SetText(ocpVars.User)
	ocpAccessTokenEntry.SetText(ocpVars.Token)
	ocpApiUrlEntry.SetText(ocpVars.Url)

	setWidgets()

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

// returns the correct ocp login/logout button
func setWidgets() {
	if ocp.IsLoggedIn() {
		retLogoutBtn()
		ocpUserNameEntry.Disable()
		ocpAccessTokenEntry.Disable()
		ocpApiUrlEntry.Disable()
	} else {
		retLoginBtn()
	}
}

func retLoginBtn() {

	ocpFormButton.SetText("Log in to Cluster")
	ocpFormButton.OnTapped = func() {
		if (len(ocpUserNameEntry.Text) + len(ocpAccessTokenEntry.Text) + len(ocpApiUrlEntry.Text)) < 3 {
			if ocpUserNameEntry.Text == "" {
				ocp.BLogs.Append("No username provided")
			}
			if ocpAccessTokenEntry.Text == "" {
				ocp.BLogs.Append("No password provided")
			}
			if ocpApiUrlEntry.Text == "" {
				ocp.BLogs.Append("No url provided")
			}
		} else {
			ocp.Login(ocpUserNameEntry.Text, ocpAccessTokenEntry.Text, ocpApiUrlEntry.Text)
			// if ocp.IsLoggedIn() {
			// 	ocp.BLogs.Append("Successful Connection")
			// }
			ocpUserNameEntry.Disable()
			ocpAccessTokenEntry.Disable()
			ocpApiUrlEntry.Disable()
			retLogoutBtn()
			ocpFormButton.Refresh()
		}
	}
}

func retLogoutBtn() {
	ocpFormButton.SetText("Logout OCP Cluster")
	ocpFormButton.OnTapped = func() {
		if ocp.Logout() {
			// ocp.BLogs.Append("Logged out Successfully")
			retLoginBtn()
			ocpUserNameEntry.Enable()
			ocpAccessTokenEntry.Enable()
			ocpApiUrlEntry.Enable()
			ocpFormButton.Refresh()
		}
		// else {
		// 	ocp.BLogs.Append("Log out Unsuccessful, Please see logs for more information")
		// }
	}
}

// func Deployment() fyne.CanvasObject {
// 	return canvas.NewText("Deployment objects go here", color.RGBA{0, 0, 255, 1})
// }
