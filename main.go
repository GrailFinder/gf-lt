package main

import (
	"flag"
	"strconv"
	"unicode"

	"github.com/rivo/tview"
)

var (
	botRespMode   = false
	editMode      = false
	injectRole    = true
	selectedIndex = int(-1)
	// indexLine           = "F12 to show keys help | bot resp mode: [orange:-:b]%v[-:-:-] (F6) | card's char: [orange:-:b]%s[-:-:-] (ctrl+s) | chat: [orange:-:b]%s[-:-:-] (F1) | toolUseAdviced: [orange:-:b]%v[-:-:-] (ctrl+k) | model: [orange:-:b]%s[-:-:-] (ctrl+l) | skip LLM resp: [orange:-:b]%v[-:-:-] (F10)\nAPI_URL: [orange:-:b]%s[-:-:-] (ctrl+v) | ThinkUse: [orange:-:b]%v[-:-:-] (ctrl+p) | Log Level: [orange:-:b]%v[-:-:-] (ctrl+p) | Recording: [orange:-:b]%v[-:-:-] (ctrl+r) | Writing as: [orange:-:b]%s[-:-:-] (ctrl+q)"
	indexLineCompletion = "F12 to show keys help | bot resp mode: [orange:-:b]%v[-:-:-] (F6) | card's char: [orange:-:b]%s[-:-:-] (ctrl+s) | chat: [orange:-:b]%s[-:-:-] (F1) | toolUseAdviced: [orange:-:b]%v[-:-:-] (ctrl+k) | model: [orange:-:b]%s[-:-:-] (ctrl+l) | skip LLM resp: [orange:-:b]%v[-:-:-] (F10)\nAPI_URL: [orange:-:b]%s[-:-:-] (ctrl+v) | ThinkUse: [orange:-:b]%v[-:-:-] (ctrl+p) | Log Level: [orange:-:b]%v[-:-:-] (ctrl+p) | Recording: [orange:-:b]%v[-:-:-] (ctrl+r) | Writing as: [orange:-:b]%s[-:-:-] (ctrl+q) | Bot will write as [orange:-:b]%s[-:-:-] (ctrl+x) | role_inject [orange:-:b]%v[-:-:-]"
	focusSwitcher       = map[tview.Primitive]tview.Primitive{}
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
	apiPort := flag.Int("port", 0, "port to host api")
	flag.Parse()
	if apiPort != nil && *apiPort > 3000 {
		srv := Server{}
		srv.ListenToRequests(strconv.Itoa(*apiPort))
		return
	}
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(true).EnablePaste(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}
