package main

import (
	"flag"
	"fmt"
	"net/http"
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
	apiPort := flag.Int("port", 0, "port to host api")
	flag.Parse()
	if apiPort != nil && *apiPort > 3000 {
		// start api server
		http.HandleFunc("POST /completion", completion)
		http.ListenAndServe(fmt.Sprintf(":%d", *apiPort), nil)
		// no tui
		return
	}
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(true).EnablePaste(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}
