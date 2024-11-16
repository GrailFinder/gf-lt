package main

import (
	"fmt"
	"path"
	"time"
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
	pages := tview.NewPages()
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
	chatOpts := []string{"cancel", "new"}
	fList, err := listHistoryFiles(historyDir)
	if err != nil {
		panic(err)
	}
	chatOpts = append(chatOpts, fList...)
	modal := tview.NewModal().
		SetText("Chat actions:").
		AddButtons(chatOpts).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonLabel {
			case "new":
				// set chat body
				chatBody.Messages = defaultStarter
				textView.SetText(chatToText(showSystemMsgs))
				chatFileLoaded = path.Join(historyDir, fmt.Sprintf("%d_chat.json", time.Now().Unix()))
				pages.RemovePage("history")
				return
			// set text
			case "cancel":
				pages.RemovePage("history")
				// pages.ShowPage("main")
				return
			default:
				// fn := path.Join(historyDir, buttonLabel)
				fn := buttonLabel
				history, err := readHistoryChat(fn)
				if err != nil {
					logger.Error("failed to read history file", "filename", fn)
					pages.RemovePage("history")
					return
				}
				chatBody.Messages = history
				textView.SetText(chatToText(showSystemMsgs))
				chatFileLoaded = fn
				pages.RemovePage("history")
				return
			}
		})
	textArea.SetMovedFunc(updateStatusLine)
	updateStatusLine()
	textView.SetText(chatToText(showSystemMsgs))
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if botRespMode {
			// do nothing while bot typing
			return nil
		}
		if event.Key() == tcell.KeyF1 {
			fList, err := listHistoryFiles(historyDir)
			if err != nil {
				panic(err)
			}
			chatOpts = append(chatOpts, fList...)
			pages.AddPage("history", modal, true, true)
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
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
