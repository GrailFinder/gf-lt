package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	normalMode  = false
	botRespMode = false
	botMsg      = "no"
	indexLine   = "Row: [yellow]%d[white], Column: [yellow]%d; normal mode: %v"
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
	app := tview.NewApplication()
	textArea := tview.NewTextArea().
		SetPlaceholder("Type your prompt...")
	textArea.SetBorder(true).SetTitle("input")
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			app.Draw()
		})
	textView.SetBorder(true).SetTitle("chat")
	position := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textView, 0, 40, false).
		AddItem(textArea, 0, 10, true).
		AddItem(position, 0, 1, false)
	updateStatusLine := func() {
		fromRow, fromColumn, toRow, toColumn := textArea.GetCursor()
		if fromRow == toRow && fromColumn == toColumn {
			position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, normalMode))
		} else {
			position.SetText(fmt.Sprintf("[red]From[white] Row: [yellow]%d[white], Column: [yellow]%d[white] - [red]To[white] Row: [yellow]%d[white], To Column: [yellow]%d; normal mode: %v", fromRow, fromColumn, toRow, toColumn, normalMode))
		}
	}
	textArea.SetMovedFunc(updateStatusLine)
	updateStatusLine()
	history := chatToText()
	chatHistory := strings.Builder{}
	for _, m := range history {
		chatHistory.WriteString(m)
		// textView.SetText(m)
	}
	textView.SetText(chatHistory.String())
	// textView.SetText("<🤖>: Hello! What can I do for you?")
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if botRespMode {
			// do nothing while bot typing
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			fromRow, fromColumn, _, _ := textArea.GetCursor()
			position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, normalMode))
			// read all text into buffer
			msgText := textArea.GetText()
			if msgText != "" {
				fmt.Fprintf(textView, "\n<user>: %s\n", msgText)
				textArea.SetText("", true)
			}
			// update statue line
			go chatRound(msgText, userRole, textView)
			return nil
		}
		if isASCII(string(event.Rune())) {
			// normalMode = false
			// fromRow, fromColumn, _, _ := textArea.GetCursor()
			// position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, normalMode))
			return event
		}
		return event
	})
	if err := app.SetRoot(flex,
		true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
