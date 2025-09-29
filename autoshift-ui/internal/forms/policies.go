package forms

import (
	"asui/internal/data_io"
	"asui/internal/structs"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var policies = structs.CreatePolicies()

func Policies() fyne.CanvasObject {

	data_io.ReadPolicies()

	policyCheckGroup := container.NewGridWithColumns(2)

	for i, _ := range policies.Policies {
		policyCheckGroup.Add(
			policyCard(&policies.Policies[i]),
		)
	}

	return container.NewVScroll(policyCheckGroup)
}

func policyCard(policy *structs.Policy) *widget.Card {

	contents := container.New(layout.NewStackLayout(),
		widget.NewLabel(policy.Desc),
		widget.NewCheck("Install", func(b bool) {
			policy.UpdateIsSelected()
		}),
	)

	return widget.NewCard(
		policy.Name,
		policy.Policy_type,
		contents,
	)
}
