package forms

import (
	"asui/internal/data_io"
	"asui/internal/structs"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var clusterSetTabs container.AppTabs

func Policies(win fyne.Window) fyne.CanvasObject {
	mainWin = win

	updateTabs()

	AddClusterSetBtn := widget.NewButton("Add New ClusterSet", func() { addHubClusterSet() })
	clusterSetContainer := container.NewVBox(&clusterSetTabs, AddClusterSetBtn)

	return clusterSetContainer
}

func updateTabs() {
	clusterSetTabs = container.AppTabs{}
	data_io.ReadPolicies()

	managedPols := policies.GetManagedPolicies()

	clusterSetTabs.Append(container.NewTabItem("managed", createPolicyCheckGroup(&managedPols)))

	for k, v := range policies.GetHubPolicies() {
		clusterSetTabs.Append(container.NewTabItem(k, createPolicyCheckGroup(&v)))
	}
}

func createPolicyCheckGroup(pols *[]structs.Policy) fyne.CanvasObject {
	policyCheckGroup := container.NewGridWithColumns(2)

	for _, v := range *pols {
		policyCheckGroup.Add(
			policyCard(&v),
		)
	}

	policyCheckGroupScroll := container.NewVScroll(policyCheckGroup)
	policyCheckGroupScroll.SetMinSize(fyne.NewSize(policyCheckGroup.Size().Width, 500))

	return policyCheckGroupScroll
}

// func addManagedLabels() {

// }

func policyCard(policy *structs.Policy) *widget.Card {

	configsModalBtn := widget.NewButton(
		"Update Configs",
		func() {
			showUpdateConfigsModal(policy)
		})
	configsModalBtn.Disable()
	contents := container.New(layout.NewStackLayout(),
		widget.NewLabel(policy.Desc),
		container.NewGridWithColumns(2,
			widget.NewCheck("Install", func(b bool) {
				if b {
					configsModalBtn.Enable()
				} else {
					configsModalBtn.Disable()
				}
				policy.UpdateIsSelected()
			}), configsModalBtn,
		),
	)

	return widget.NewCard(
		policy.Name,
		policy.Policy_type,
		contents,
	)
}

// func showAddClusterSetModal() fyne.CanvasObject {

// 	// nameLabel := widget.NewLabel("Name: ")
// 	nameEntry := widget.NewEntry()
// 	// typeLabel := widget.NewLabel("ClusterSet Type: ")
// 	typeOpts := widget.NewRadioGroup([]string{"hub", "managed"}, func(s string) {})
// 	// dconfBtn := widget.NewButton("Add ClusterSet", func() {})

//    dialog.ShowForm("New ClusterSet","Create ClusterSet","Cancel",[]*widget.FormItem{
// 		widget.NewFormItem("ClusterSet Name:", nameEntry),
// 		widget.NewFormItem("ClusterSet Type:",typeOpts),
//    },func(b bool){

//    },mainWin)
// }
