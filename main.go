package main

import (
	"github.com/rivo/tview"
)

var (
	boolColors        = map[bool]string{true: "green", false: "red"}
	botRespMode       = false
	toolRunningMode   = false
	editMode          = false
	roleEditMode      = false
	injectRole        = true
	selectedIndex     = int(-1)
	shellMode         = false
	shellHistory      []string
	shellHistoryPos   int = -1
	thinkingCollapsed     = false
	toolCollapsed         = false
	statusLineTempl       = "help (F12) | chat: [orange:-:b]%s[-:-:-] (F1) | [%s:-:b]tool use[-:-:-] (ctrl+k) | model: [%s:-:b]%s[-:-:-] (ctrl+l) | [%s:-:b]skip LLM resp[-:-:-] (F10)\nAPI: [orange:-:b]%s[-:-:-] (ctrl+v) | writing as: [orange:-:b]%s[-:-:-] (ctrl+q) | bot will write as [orange:-:b]%s[-:-:-] (ctrl+x)"
	focusSwitcher         = map[tview.Primitive]tview.Primitive{}
)

func main() {
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(cfg.EnableMouse).EnablePaste(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}
