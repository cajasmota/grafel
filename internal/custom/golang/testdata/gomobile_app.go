//go:build android || ios

package main

import (
	"runtime"

	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/gl"
)

func main() {
	app.Main(func(a app.App) {
		for e := range a.Events() {
			switch e.(type) {
			case lifecycle.Event:
				_ = gl.Version
			}
		}
		if runtime.GOOS == "android" {
			_ = "android-only"
		}
	})
}
