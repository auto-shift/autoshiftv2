package forms

import (
	"asui/internal/io"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func Policies(win fyne.Window) fyne.CanvasObject {
	policies := []string{}

	for _, value := range io.GetPolicies().Policies {
		policies = append(policies, value.Name)
	}

	infraCheckGroup := widget.NewCheckGroup(policies, func([]string) {})

	policies_form := container.NewVBox(infraCheckGroup)

	return policies_form
}
