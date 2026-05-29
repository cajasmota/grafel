package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("Hello Fyne")

	label := widget.NewLabel("Welcome")
	entry := widget.NewEntry()
	btn := widget.NewButton("Click", func() {
		label.SetText("clicked")
	})

	// event-handler wiring
	btn.OnTapped = func() {
		label.SetText("tapped")
	}
	entry.OnChanged = func(s string) {
		label.SetText(s)
	}
	w.SetOnClosed(func() {
		// cleanup
	})

	box := container.NewVBox(label, entry, btn)
	w.SetContent(box)
	w.ShowAndRun()
}
