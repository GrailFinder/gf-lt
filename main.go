package main

import (
	"unicode"

	"github.com/rivo/tview"
)

var (
	botRespMode   = false
	editMode      = false
	botMsg        = "no"
	selectedIndex = int(-1)
	indexLine     = "Esc: send msg; PgUp/Down: switch focus; F1: manage chats; F2: regen last; F3:delete last msg; F4: edit msg; F5: toggle system; F6: interrupt bot resp; bot resp mode: %v; current chat: %s"
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
		true).EnableMouse(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}
