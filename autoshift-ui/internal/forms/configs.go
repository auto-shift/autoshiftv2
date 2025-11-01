package forms

import (
	"asui/internal/data_io"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

func Configs() fyne.CanvasObject {

	data_io.SourceTestInputs()

	configsGrid := container.NewGridWithRows(
		2,
		container.NewGridWithColumns(2, gitConfigs(), deploymentConfigs()),
		container.NewStack(OutputCard()),
	)
	return configsGrid
}

// func logList() fyne.CanvasObject {

// 	listLbl :=
// 	return listLbl
// }
