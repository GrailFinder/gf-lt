package main

import (
	"unicode"

	"github.com/rivo/tview"
)

var (
	botRespMode   = false
	editMode      = false
	selectedIndex = int(-1)
	indexLine     = "F12 to show keys help; bot resp mode: %v; char: %s; chat: %s; RAGEnabled: %v; toolUseAdviced: %v; model: %s\nAPI_URL: %s"
	focusSwitcher = map[tview.Primitive]tview.Primitive{}
)

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func main() {
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(true).EnablePaste(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}
