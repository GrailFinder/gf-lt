package main

import (
	"fmt"
	"path"
	"strconv"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	normalMode    = false
	botRespMode   = false
	editMode      = false
	botMsg        = "no"
	selectedIndex = int(-1)
	indexLine     = "Esc: send msg; Tab: switch focus; F1: manage chats; F2: regen last; F3:delete msg menu; F4: edit msg; F5: toggle system; Row: [yellow]%d[white], Column: [yellow]%d; normal mode: %v"
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
	focusSwitcher[textArea] = textView
	focusSwitcher[textView] = textArea
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
			position.SetText(fmt.Sprintf("Esc: send msg; Tab: switch focus; F1: manage chats; F2: regen last; F3:delete msg menu; F4: edit msg; F5: toggle system; Row: [yellow]%d[white], Column: [yellow]%d[white] - [red]To[white] Row: [yellow]%d[white], To Column: [yellow]%d; normal mode: %v", fromRow, fromColumn, toRow, toColumn, normalMode))
		}
	}
	chatOpts := []string{"cancel", "new"}
	fList, err := listHistoryFiles(historyDir)
	if err != nil {
		panic(err)
	}
	chatOpts = append(chatOpts, fList...)
	chatActModal := tview.NewModal().
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
	editArea := tview.NewTextArea().
		SetPlaceholder("Replace msg...")
	editArea.SetBorder(true).SetTitle("input")
	editArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape && editMode {
			editedMsg := editArea.GetText()
			// TODO: trim msg number and icon
			chatBody.Messages[selectedIndex].Content = editedMsg
			// change textarea
			textView.SetText(chatToText(showSystemMsgs))
			pages.RemovePage("editArea")
			editMode = false
			// panic("do we get here?")
			// pages.ShowPage("main")
			return nil
		}
		return event
	})
	indexPickWindow := tview.NewInputField().
		SetLabel("Enter a msg index: ").
		SetFieldWidth(4).
		SetAcceptanceFunc(tview.InputFieldInteger).
		SetDoneFunc(func(key tcell.Key) {
			pages.RemovePage("getIndex")
			return
		})
	indexPickWindow.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			si := indexPickWindow.GetText()
			selectedIndex, err = strconv.Atoi(si)
			if err != nil {
				logger.Error("failed to convert provided index", "error", err, "si", si)
			}
			if len(chatBody.Messages) <= selectedIndex && selectedIndex < 0 {
				logger.Warn("chosen index is out of bounds", "index", selectedIndex)
				return nil
			}
			pages.AddPage("editArea", editArea, true, true)
			m := chatBody.Messages[selectedIndex]
			// editArea.SetText(m.ToText(selectedIndex), true)
			editArea.SetText(m.Content, true)
			editMode = true
			// editArea.SetText(si, true)
		}
		return event
	})
	//
	textArea.SetMovedFunc(updateStatusLine)
	updateStatusLine()
	textView.SetText(chatToText(showSystemMsgs))
	textView.ScrollToEnd()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyF1 {
			fList, err := listHistoryFiles(historyDir)
			if err != nil {
				panic(err)
			}
			chatOpts = append(chatOpts, fList...)
			pages.AddPage("history", chatActModal, true, true)
			return nil
		}
		if event.Key() == tcell.KeyF2 {
			// regen last msg
			chatBody.Messages = chatBody.Messages[:len(chatBody.Messages)-1]
			textView.SetText(chatToText(showSystemMsgs))
			go chatRound("", userRole, textView)
			return nil
		}
		if event.Key() == tcell.KeyF3 {
			// TODO: delete last n messages
			// modal window with input field
			chatBody.Messages = chatBody.Messages[:len(chatBody.Messages)-1]
			textView.SetText(chatToText(showSystemMsgs))
			botRespMode = false // hmmm; is that correct?
			return nil
		}
		if event.Key() == tcell.KeyF4 {
			// edit msg
			pages.AddPage("getIndex", indexPickWindow, true, true)
			editMode = true
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			// switch showSystemMsgs
			showSystemMsgs = !showSystemMsgs
			textView.SetText(chatToText(showSystemMsgs))
		}
		// cannot send msg in editMode or botRespMode
		if event.Key() == tcell.KeyEscape && !editMode && !botRespMode {
			fromRow, fromColumn, _, _ := textArea.GetCursor()
			position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, normalMode))
			// read all text into buffer
			msgText := textArea.GetText()
			if msgText != "" {
				fmt.Fprintf(textView, "\n(%d) <user>: %s\n", len(chatBody.Messages), msgText)
				textArea.SetText("", true)
				textView.ScrollToEnd()
			}
			// update statue line
			go chatRound(msgText, userRole, textView)
			return nil
		}
		if event.Key() == tcell.KeyTab {
			currentF := app.GetFocus()
			app.SetFocus(focusSwitcher[currentF])
		}
		if isASCII(string(event.Rune())) && !botRespMode {
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
