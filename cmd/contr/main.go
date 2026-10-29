// Command contr is a native herdr client, written in Go with MyGo's native UI.
// It opens the frame window. docs/PORT-PLAN.md lists what the port adds next.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app"
)

func main() {
	app.InitActions()
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "contr",
			Width:   1440,
			Height:  900,
			Content: ui.View(app.Frame),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
