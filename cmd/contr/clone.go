package main

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app"
)

// cloneParent is the folder that holds a clone when no project was opened
// before (D117): the first of
// ~/Developer, ~/code and ~/Documents/code that exists, else the home folder.
// It is empty when the home folder is unknown.
func cloneParent(home string) string {
	if home == "" {
		return ""
	}
	for _, d := range []string{"Developer", "code", filepath.Join("Documents", "code")} {
		p := filepath.Join(home, d)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return home
}

// pickCloneParent asks the owner for the folder that holds a clone (D117). It
// blocks, so the modal calls it off the UI thread.
func pickCloneParent(def string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:             "Clone into…",
		ButtonLabel:       "Choose",
		Directory:         true,
		CreateDirectories: true,
		DefaultPath:       def,
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", app.ErrPickCancelled
	}
	return paths[0], nil
}
