package forms

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

func Deployment() fyne.CanvasObject {
	return canvas.NewText("Deployment objects go here", color.RGBA{0, 0, 255, 1})
}
