package forms

import (
	"asui/internal/data_io"
	"asui/internal/structs"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func showUpdateConfigsModal(policy *structs.Policy) {
	labels := allLabels[policy.Alias]

	formItems := []*widget.FormItem{}

	for k, v := range labels {
		// policyLabels.AddLabel(k, v)
		binding := binding.NewString()
		binding.Set(v)
		formItems = append(formItems,
			widget.NewFormItem(
				k, widget.NewEntryWithData(binding),
			),
		)
	}

	// policyLabels.GetLabels()

	formDialog := dialog.NewForm(
		"Update Labels:", "Update", "Close", formItems, func(b bool) {}, mainWin,
	)

	formDialog.Resize(fyne.NewSize((mainWin.Canvas().Size().Width)/1.5, (mainWin.Canvas().Size().Height)/2))

	formDialog.Show()
}

func addHubClusterSet() {
	nameEntry := widget.NewEntry()
	nameEntry.Resize(fyne.NewSize(500, nameEntry.Size().Height))

	csFormDialog := dialog.NewForm("New ClusterSet", "Create", "Cancel", []*widget.FormItem{
		widget.NewFormItem("ClusterSet Name:", nameEntry),
	}, func(b bool) {
		if b {
			policies.AddClusterSet(nameEntry.Text)
			policies.AddHubPolicies(nameEntry.Text, data_io.ReadPolicyList())
			fmt.Println("hub policies:")
			fmt.Println(policies.GetHubPolicies())
			data_io.WritePolicies(policies)
			data_io.ReadPolicies()
			updateTabs()
			mainWin.Canvas().Content().Refresh()
		}
	}, mainWin)

	csFormDialog.Show()
}
