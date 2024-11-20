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
	botRespMode   = false
	editMode      = false
	botMsg        = "no"
	selectedIndex = int(-1)
	indexLine     = "Esc: send msg; Tab: switch focus; F1: manage chats; F2: regen last; F3:delete last msg; F4: edit msg; F5: toggle system; F6: interrupt bot resp; Row: [yellow]%d[white], Column: [yellow]%d; bot resp mode: %v"
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
			position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, botRespMode))
		} else {
			position.SetText(fmt.Sprintf("Esc: send msg; Tab: switch focus; F1: manage chats; F2: regen last; F3:delete last msg; F4: edit msg; F5: toggle system; F6: interrupt bot resp; Row: [yellow]%d[white], Column: [yellow]%d[white] - [red]To[white] Row: [yellow]%d[white], To Column: [yellow]%d; bot resp mode: %v", fromRow, fromColumn, toRow, toColumn, botRespMode))
		}
	}
	chatOpts := []string{"cancel", "new"}
	fList, err := loadHistoryChats()
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
				activeChatName = path.Join(historyDir, fmt.Sprintf("%d_chat.json", time.Now().Unix()))
				pages.RemovePage("history")
				return
			// set text
			case "cancel":
				pages.RemovePage("history")
				return
			default:
				fn := buttonLabel
				history, err := loadHistoryChat(fn)
				if err != nil {
					logger.Error("failed to read history file", "filename", fn)
					pages.RemovePage("history")
					return
				}
				chatBody.Messages = history
				textView.SetText(chatToText(showSystemMsgs))
				activeChatName = fn
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
			if editedMsg == "" {
				notifyUser("edit", "no edit provided")
				pages.RemovePage("editArea")
				editMode = false
				return nil
			}
			chatBody.Messages[selectedIndex].Content = editedMsg
			// change textarea
			textView.SetText(chatToText(showSystemMsgs))
			pages.RemovePage("editArea")
			editMode = false
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
		si := indexPickWindow.GetText()
		selectedIndex, err = strconv.Atoi(si)
		if err != nil {
			logger.Error("failed to convert provided index", "error", err, "si", si)
		}
		if len(chatBody.Messages) <= selectedIndex && selectedIndex < 0 {
			logger.Warn("chosen index is out of bounds", "index", selectedIndex)
			return nil
		}
		m := chatBody.Messages[selectedIndex]
		if editMode && event.Key() == tcell.KeyEnter {
			pages.AddPage("editArea", editArea, true, true)
			editArea.SetText(m.Content, true)
		}
		if !editMode && event.Key() == tcell.KeyEnter {
			// TODO: add notification that text was copied
			copyToClipboard(m.Content)
			notification := fmt.Sprintf("msg '%s' was copied to the clipboard", m.Content[:30])
			notifyUser("copied", notification)
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
			// fList, err := listHistoryFiles(historyDir)
			fList, err := loadHistoryChats()
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
			editMode = true
			pages.AddPage("getIndex", indexPickWindow, true, true)
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			// switch showSystemMsgs
			showSystemMsgs = !showSystemMsgs
			textView.SetText(chatToText(showSystemMsgs))
		}
		if event.Key() == tcell.KeyF6 {
			interruptResp = true
			botRespMode = false
			return nil
		}
		if event.Key() == tcell.KeyF7 {
			// copy msg to clipboard
			editMode = false
			pages.AddPage("getIndex", indexPickWindow, true, true)
			return nil
		}
		// cannot send msg in editMode or botRespMode
		if event.Key() == tcell.KeyEscape && !editMode && !botRespMode {
			fromRow, fromColumn, _, _ := textArea.GetCursor()
			position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, botRespMode))
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
			// botRespMode = false
			// fromRow, fromColumn, _, _ := textArea.GetCursor()
			// position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, botRespMode))
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
