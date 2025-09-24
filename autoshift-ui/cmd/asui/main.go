package main

import (
	"asui/internal/app_main"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

func main() {
	app := app.New()
	win := app.NewWindow("AutoShift UI")
	win.Resize(fyne.NewSize(800, 600))
	win.SetContent(app_main.Home(win))
	win.ShowAndRun()
}

func tidyup() {}
