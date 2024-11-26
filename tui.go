package main

import (
	"elefant/models"
	"fmt"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	app             *tview.Application
	pages           *tview.Pages
	textArea        *tview.TextArea
	editArea        *tview.TextArea
	textView        *tview.TextView
	position        *tview.TextView
	helpView        *tview.TextView
	flex            *tview.Flex
	chatActModal    *tview.Modal
	sysModal        *tview.Modal
	indexPickWindow *tview.InputField
	renameWindow    *tview.InputField
	helpText        = `
[yellow]Esc[white]: send msg
[yellow]PgUp/Down[white]: switch focus
[yellow]F1[white]: manage chats
[yellow]F2[white]: regen last
[yellow]F3[white]: delete last msg
[yellow]F4[white]: edit msg
[yellow]F5[white]: toggle system
[yellow]F6[white]: interrupt bot resp
[yellow]F7[white]: copy msg to clipboard (linux xclip)
[yellow]Ctrl+s[white]: choose/replace system prompt

Press Enter to go back
`
)

func init() {
	app = tview.NewApplication()
	pages = tview.NewPages()
	textArea = tview.NewTextArea().
		SetPlaceholder("Type your prompt...")
	textArea.SetBorder(true).SetTitle("input")
	textView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			app.Draw()
		})
	textView.SetBorder(true).SetTitle("chat")
	focusSwitcher[textArea] = textView
	focusSwitcher[textView] = textArea
	position = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textView, 0, 40, false).
		AddItem(textArea, 0, 10, true).
		AddItem(position, 0, 1, false)
	updateStatusLine := func() {
		fromRow, fromColumn, toRow, toColumn := textArea.GetCursor()
		if fromRow == toRow && fromColumn == toColumn {
			position.SetText(fmt.Sprintf(indexLine, botRespMode, activeChatName))
		} else {
			position.SetText(fmt.Sprintf("Esc: send msg; PgUp/Down: switch focus; F1: manage chats; F2: regen last; F3:delete last msg; F4: edit msg; F5: toggle system; F6: interrupt bot resp; Row: [yellow]%d[white], Column: [yellow]%d[white] - [red]To[white] Row: [yellow]%d[white], To Column: [yellow]%d; bot resp mode: %v", fromRow, fromColumn, toRow, toColumn, botRespMode))
		}
	}
	chatOpts := []string{"cancel", "new", "rename current"}
	chatList, err := loadHistoryChats()
	if err != nil {
		logger.Error("failed to load chat history", "error", err)
		chatList = []string{}
	}
	chatActModal := tview.NewModal().
		SetText("Chat actions:").
		AddButtons(append(chatOpts, chatList...)).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonLabel {
			case "new":
				id, err := store.ChatGetMaxID()
				if err != nil {
					logger.Error("failed to get chat id", "error", err)
				}
				// set chat body
				chatBody.Messages = defaultStarter
				textView.SetText(chatToText(showSystemMsgs))
				newChat := &models.Chat{
					ID:   id + 1,
					Name: fmt.Sprintf("%v_%v", "new", time.Now().Unix()),
					Msgs: string(defaultStarterBytes),
				}
				// activeChatName = path.Join(historyDir, fmt.Sprintf("%d_chat.json", time.Now().Unix()))
				activeChatName = newChat.Name
				chatMap[newChat.Name] = newChat
				pages.RemovePage("history")
				return
			// set text
			case "cancel":
				pages.RemovePage("history")
				return
			case "rename current":
				// add input field
				pages.RemovePage("history")
				pages.AddPage("renameW", renameWindow, true, true)
				return
			default:
				fn := buttonLabel
				history, err := loadHistoryChat(fn)
				if err != nil {
					logger.Error("failed to read history file", "chat", fn)
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
	sysModal = tview.NewModal().
		SetText("Switch sys msg:").
		AddButtons(sysLabels).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonLabel {
			case "cancel":
				pages.RemovePage("sys")
				return
			default:
				sysMsg, ok := sysMap[buttonLabel]
				if !ok {
					logger.Warn("no such sys msg", "name", buttonLabel)
					pages.RemovePage("sys")
					return
				}
				chatBody.Messages[0].Content = sysMsg
				// replace textview
				textView.SetText(chatToText(showSystemMsgs))
				pages.RemovePage("sys")
			}
		})
	editArea = tview.NewTextArea().
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
	indexPickWindow = tview.NewInputField().
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
			copyToClipboard(m.Content)
			notification := fmt.Sprintf("msg '%s' was copied to the clipboard", m.Content[:30])
			notifyUser("copied", notification)
		}
		return event
	})
	//
	renameWindow = tview.NewInputField().
		SetLabel("Enter a msg index: ").
		SetFieldWidth(20).
		SetAcceptanceFunc(tview.InputFieldMaxLength(100)).
		SetDoneFunc(func(key tcell.Key) {
			pages.RemovePage("renameW")
			return
		})
	renameWindow.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			nname := renameWindow.GetText()
			if nname == "" {
				return event
			}
			currentChat := chatMap[activeChatName]
			delete(chatMap, activeChatName)
			currentChat.Name = nname
			activeChatName = nname
			chatMap[activeChatName] = currentChat
			_, err := store.UpsertChat(currentChat)
			if err != nil {
				logger.Error("failed to upsert chat", "error", err, "chat", currentChat)
			}
			notification := fmt.Sprintf("renamed chat to '%s'", activeChatName)
			notifyUser("renamed", notification)
		}
		return event
	})
	//
	helpView = tview.NewTextView().SetDynamicColors(true).SetText(helpText).SetDoneFunc(func(key tcell.Key) {
		pages.RemovePage("helpView")
		return
	})
	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyEnter:
			return event
		}
		return nil
	})
	//
	textArea.SetMovedFunc(updateStatusLine)
	updateStatusLine()
	textView.SetText(chatToText(showSystemMsgs))
	textView.ScrollToEnd()
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyF1 {
			chatList, err := loadHistoryChats()
			if err != nil {
				logger.Error("failed to load chat history", "error", err)
				return nil
			}
			chatOpts := append(chatOpts, chatList...)
			chatActModal.ClearButtons()
			chatActModal.AddButtons(chatOpts)
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
			// delete last msg
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
			m := chatBody.Messages[len(chatBody.Messages)-1]
			copyToClipboard(m.Content)
			notification := fmt.Sprintf("msg '%s' was copied to the clipboard", m.Content[:30])
			notifyUser("copied", notification)
			return nil
		}
		if event.Key() == tcell.KeyF8 {
			// copy msg to clipboard
			editMode = false
			pages.AddPage("getIndex", indexPickWindow, true, true)
			return nil
		}
		if event.Key() == tcell.KeyF12 {
			// help window cheatsheet
			pages.AddPage("helpView", helpView, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlE {
			textArea.SetText("pressed ctrl+e", true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			// switch sys prompt
			pages.AddPage("sys", sysModal, true, true)
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
		if event.Key() == tcell.KeyPgUp || event.Key() == tcell.KeyPgDn {
			currentF := app.GetFocus()
			app.SetFocus(focusSwitcher[currentF])
			return nil
		}
		if isASCII(string(event.Rune())) && !botRespMode {
			// botRespMode = false
			// fromRow, fromColumn, _, _ := textArea.GetCursor()
			// position.SetText(fmt.Sprintf(indexLine, fromRow, fromColumn, botRespMode))
			return event
		}
		return event
	})
}
