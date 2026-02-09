package main

import (
	"flag"
	"strconv"

	"github.com/rivo/tview"
)

var (
	boolColors          = map[bool]string{true: "green", false: "red"}
	botRespMode         = false
	editMode            = false
	roleEditMode        = false
	injectRole          = true
	selectedIndex       = int(-1)
	shellMode           = false
	indexLineCompletion = "F12 to show keys help | llm turn: [%s:-:b]%v[-:-:-] (F6) | chat: [orange:-:b]%s[-:-:-] (F1) | toolUseAdviced: [%s:-:b]%v[-:-:-] (ctrl+k) | model: [orange:-:b]%s[-:-:-] (ctrl+l) | skip LLM resp: [%s:-:b]%v[-:-:-] (F10)\nAPI: [orange:-:b]%s[-:-:-] (ctrl+v) | recording: [%s:-:b]%v[-:-:-] (ctrl+r) | writing as: [orange:-:b]%s[-:-:-] (ctrl+q) | bot will write as [orange:-:b]%s[-:-:-] (ctrl+x) | role injection (alt+7) [%s:-:b]%v[-:-:-]"
	focusSwitcher       = map[tview.Primitive]tview.Primitive{}
)

func main() {
	apiPort := flag.Int("port", 0, "port to host api")
	flag.Parse()
	if apiPort != nil && *apiPort > 3000 {
		srv := Server{}
		srv.ListenToRequests(strconv.Itoa(*apiPort))
		return
	}
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(cfg.EnableMouse).EnablePaste(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}
