package forms

import (
	"asui/internal/ocp"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
)

var logList *widget.List

func OutputCard() fyne.CanvasObject {
	logList = widget.NewListWithData(ocp.BLogs,
		func() fyne.CanvasObject {
			// Template for each list item
			listItem := widget.NewLabel("Placeholder")
			listItem.Wrapping = fyne.TextWrapWord
			return listItem
		},
		func(i binding.DataItem, o fyne.CanvasObject) {
			// Update the template with the actual data
			strBinding := i.(binding.String) // Cast DataItem to String binding
			label := o.(*widget.Label)
			label.Bind(strBinding) // Bind the label directly to the string binding
		},
	)

	return widget.NewCard("output: ", "",
		container.NewVScroll(logList),
	)
}

// logList.Refresh()
// logList.ScrollToBottom()
