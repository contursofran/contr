// Command contr is a native herdr client, written in Go with MyGo's native UI.
// It opens the frame window. docs/PORT-PLAN.md lists what the port adds next.
package main

import (
	"context"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/herdr"
)

func main() {
	app.InitActions()
	frame := app.NewFrame(nil)
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:         "contr",
			Width:         1440,
			Height:        900,
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(frame.View),
		})
		stop := frame.Attach(context.Background(), win, herdr.LocalTarget())
		win.OnClosed(stop)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
