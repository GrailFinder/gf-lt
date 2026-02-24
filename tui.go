package main

import (
	"fmt"
	"gf-lt/models"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func isFullScreenPageActive() bool {
	name, _ := pages.GetFrontPage()
	return name != "main"
}

var (
	app                *tview.Application
	pages              *tview.Pages
	textArea           *tview.TextArea
	editArea           *tview.TextArea
	textView           *tview.TextView
	statusLineWidget   *tview.TextView
	helpView           *tview.TextView
	flex               *tview.Flex
	imgView            *tview.Image
	defaultImage       = "sysprompts/llama.png"
	indexPickWindow    *tview.InputField
	renameWindow       *tview.InputField
	roleEditWindow     *tview.InputField
	fullscreenMode     bool
	positionVisible    bool = true
	scrollToEndEnabled bool = true
	// pages
	historyPage    = "historyPage"
	agentPage      = "agentPage"
	editMsgPage    = "editMsgPage"
	roleEditPage   = "roleEditPage"
	helpPage       = "helpPage"
	renamePage     = "renamePage"
	RAGPage        = "RAGPage"
	RAGLoadedPage  = "RAGLoadedPage"
	propsPage      = "propsPage"
	codeBlockPage  = "codeBlockPage"
	imgPage        = "imgPage"
	filePickerPage = "filePicker"
	exportDir      = "chat_exports"
	// For overlay search functionality
	searchField    *tview.InputField
	searchPageName = "searchOverlay"
	// help text
	helpText = `
[yellow]Esc[white]: send msg
[yellow]PgUp/Down[white]: switch focus between input and chat widgets
[yellow]F1[white]: manage chats
[yellow]F2[white]: regen last
[yellow]F3[white]: delete last msg
[yellow]F4[white]: edit msg
[yellow]F5[white]: toggle fullscreen for input/chat window
[yellow]F6[white]: interrupt bot resp
[yellow]F7[white]: copy last msg to clipboard (linux xclip)
[yellow]F8[white]: copy n msg to clipboard (linux xclip)
[yellow]F9[white]: table to copy from; with all code blocks
[yellow]F10[white]: switch if LLM will respond on this message (for user to write multiple messages in a row)
[yellow]F11[white]: import json chat file
[yellow]F12[white]: show this help page
[yellow]Ctrl+w[white]: resume generation on the last msg
[yellow]Ctrl+s[white]: load new char/agent
[yellow]Ctrl+e[white]: export chat to json file
[yellow]Ctrl+c[white]: close programm
[yellow]Ctrl+n[white]: start a new chat
[yellow]Ctrl+o[white]: open image file picker
[yellow]Ctrl+p[white]: props edit form (min-p, dry, etc.)
[yellow]Ctrl+v[white]: show API link selection popup to choose current API
[yellow]Ctrl+r[white]: start/stop recording from your microphone (needs stt server or whisper binary)
[yellow]Ctrl+t[white]: remove thinking (<think>) and tool messages from context (delete from chat)
[yellow]Ctrl+l[white]: show model selection popup to choose current model
[yellow]Ctrl+k[white]: switch tool use (recommend tool use to llm after user msg)
[yellow]Ctrl+a[white]: interrupt tts (needs tts server)
[yellow]Alt+0[white]: replay last message via tts (needs tts server)
[yellow]Ctrl+g[white]: open RAG file manager (load files for context retrieval)
[yellow]Ctrl+y[white]: list loaded RAG files (view and manage loaded files)
[yellow]Ctrl+q[white]: show user role selection popup to choose who sends next msg as
[yellow]Ctrl+x[white]: show bot role selection popup to choose which agent responds next
[yellow]Alt+1[white]: toggle shell mode (execute commands locally)
[yellow]Alt+2[white]: toggle auto-scrolling (for reading while LLM types)
[yellow]Alt+3[white]: summarize chat history and start new chat with summary as tool response
[yellow]Alt+4[white]: edit msg role
[yellow]Alt+5[white]: toggle system and tool messages display
[yellow]Alt+6[white]: toggle status line visibility
[yellow]Alt+7[white]: toggle role injection (inject role in messages)
[yellow]Alt+8[white]: show char img or last picked img
[yellow]Alt+9[white]: warm up (load) selected llama.cpp model
[yellow]Alt+t[white]: toggle thinking blocks visibility (collapse/expand <think> blocks)
[yellow]Alt+i[white]: show colorscheme selection popup

=== scrolling chat window (some keys similar to vim) ===
[yellow]arrows up/down and j/k[white]: scroll up and down
[yellow]gg/G[white]: jump to the begging / end of the chat
[yellow]/[white]: start searching for text
[yellow]n[white]: go to next search result
[yellow]N[white]: go to previous search result

=== tables (chat history, agent pick, file pick, properties) ===
[yellow]x[white]: to exit the table page

=== filepicker ===
[yellow]s[white]: (in file picker) set current dir as FilePickerDir
[yellow]x[white]: to exit

=== shell mode ===
	[yellow]@match->Tab[white]: file completion with relative paths (recursive, depth 3, max 50 files)

=== status line ===
%s

Press <Enter> or 'x' to return
`
)

func init() {
	// Start background goroutine to update model color cache
	startModelColorUpdater()
	tview.Styles = colorschemes["default"]
	app = tview.NewApplication()
	pages = tview.NewPages()
	textArea = tview.NewTextArea().
		SetPlaceholder("input is multiline; press <Enter> to start the next line;\npress <Esc> to send the message.")
	textArea.SetBorder(true).SetTitle("input")
	// Add input capture for @ completion
	textArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if !shellMode {
			return event
		}
		// Handle Tab key for file completion
		if event.Key() == tcell.KeyTab {
			currentText := textArea.GetText()
			row, col, _, _ := textArea.GetCursor()
			// Calculate absolute position from row/col
			lines := strings.Split(currentText, "\n")
			cursorPos := 0
			for i := 0; i < row && i < len(lines); i++ {
				cursorPos += len(lines[i]) + 1 // +1 for newline
			}
			cursorPos += col
			// Look backwards from cursor to find @
			if cursorPos > 0 {
				// Find the last @ before cursor
				textBeforeCursor := currentText[:cursorPos]
				atIndex := strings.LastIndex(textBeforeCursor, "@")
				if atIndex >= 0 {
					// Extract the partial match text after @
					filter := textBeforeCursor[atIndex+1:]
					showFileCompletionPopup(filter)
					return nil // Consume the Tab event
				}
			}
		}
		return event
	})
	textView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			app.Draw()
		})
	//
	flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textView, 0, 40, false).
		AddItem(textArea, 0, 10, true) // Restore original height
	if positionVisible {
		flex.AddItem(statusLineWidget, 0, 2, false)
	}
	// textView.SetBorder(true).SetTitle("chat")
	textView.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			if len(searchResults) > 0 { // Check if a search is active
				hideSearchBar()           // Hide the search bar if visible
				searchResults = nil       // Clear search results
				searchResultLengths = nil // Clear search result lengths
				originalTextForSearch = ""
				textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys)) // Reset text without search regions
				colorText()                                                  // Apply normal chat coloring
			} else {
				// Original logic if no search is active
				currentSelection := textView.GetHighlights()
				if len(currentSelection) > 0 {
					textView.Highlight()
				} else {
					textView.Highlight("0").ScrollToHighlight()
				}
			}
		}
	})
	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Handle vim-like navigation in TextView
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'j':
				// For line down
				return event
			case 'k':
				// For line up
				return event
			case 'g':
				// Go to beginning
				textView.ScrollToBeginning()
				return nil
			case 'G':
				// Go to end
				textView.ScrollToEnd()
				return nil
			case '/':
				// Search functionality - show search bar
				showSearchBar()
				return nil
			case 'n':
				// Next search result
				searchNext()
				return nil
			case 'N':
				// Previous search result
				searchPrev()
				return nil
			}
		}
		return event
	})
	focusSwitcher[textArea] = textView
	focusSwitcher[textView] = textArea
	statusLineWidget = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	// // vertical text center alignment
	// statusLineWidget.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
	// 	y += h / 2
	// 	return x, y, w, h
	// })
	// Initially set up flex without search bar
	flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textView, 0, 40, false).
		AddItem(textArea, 0, 10, true) // Restore original height
	if positionVisible {
		flex.AddItem(statusLineWidget, 0, 2, false)
	}
	editArea = tview.NewTextArea().
		SetPlaceholder("Replace msg...")
	editArea.SetBorder(true).SetTitle("input")
	editArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// if event.Key() == tcell.KeyEscape && editMode {
		if event.Key() == tcell.KeyEscape {
			defer colorText()
			editedMsg := editArea.GetText()
			if editedMsg == "" {
				if err := notifyUser("edit", "no edit provided"); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				pages.RemovePage(editMsgPage)
				return nil
			}
			chatBody.Messages[selectedIndex].Content = editedMsg
			// change textarea
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			pages.RemovePage(editMsgPage)
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
			hideIndexBar()
			// colorText()
			// updateStatusLine()
		})
	roleEditWindow = tview.NewInputField().
		SetLabel("Enter new role: ").
		SetPlaceholder("e.g., user, assistant, system, tool").
		SetDoneFunc(func(key tcell.Key) {
			switch key {
			case tcell.KeyEnter:
				newRole := roleEditWindow.GetText()
				if newRole == "" {
					if err := notifyUser("edit", "no role provided"); err != nil {
						logger.Error("failed to send notification", "error", err)
					}
					pages.RemovePage(roleEditPage)
					return
				}
				if selectedIndex >= 0 && selectedIndex < len(chatBody.Messages) {
					chatBody.Messages[selectedIndex].Role = newRole
					textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
					colorText()
					pages.RemovePage(roleEditPage)
				}
			case tcell.KeyEscape:
				pages.RemovePage(roleEditPage)
			}
		})
	indexPickWindow.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyBackspace:
			return event
		case tcell.KeyEscape:
			// Hide the index overlay when Escape is pressed
			hideIndexBar()
			return nil
		case tcell.KeyEnter:
			si := indexPickWindow.GetText()
			siInt, err := strconv.Atoi(si)
			if err != nil {
				logger.Error("failed to convert provided index", "error", err, "si", si)
				if err := notifyUser("cancel", "no index provided, copying user input"); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				if err := copyToClipboard(textArea.GetText()); err != nil {
					logger.Error("failed to copy to clipboard", "error", err)
				}
				hideIndexBar() // Hide overlay instead of removing page directly
				return nil
			}
			selectedIndex = siInt
			if len(chatBody.Messages)-1 < selectedIndex || selectedIndex < 0 {
				msg := "chosen index is out of bounds, will copy user input"
				logger.Warn(msg, "index", selectedIndex)
				if err := notifyUser("error", msg); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				if err := copyToClipboard(textArea.GetText()); err != nil {
					logger.Error("failed to copy to clipboard", "error", err)
				}
				hideIndexBar() // Hide overlay instead of removing page directly
				return nil
			}
			m := chatBody.Messages[selectedIndex]
			switch {
			case roleEditMode:
				hideIndexBar() // Hide overlay first
				// Set the current role as the default text in the input field
				roleEditWindow.SetText(m.Role)
				pages.AddPage(roleEditPage, roleEditWindow, true, true)
				roleEditMode = false // Reset the flag
			case editMode:
				hideIndexBar() // Hide overlay first
				pages.AddPage(editMsgPage, editArea, true, true)
				editArea.SetText(m.Content, true)
			default:
				if err := copyToClipboard(m.Content); err != nil {
					logger.Error("failed to copy to clipboard", "error", err)
				}
				previewLen := min(30, len(m.Content))
				notification := fmt.Sprintf("msg '%s' was copied to the clipboard", m.Content[:previewLen])
				if err := notifyUser("copied", notification); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				hideIndexBar() // Hide overlay after copying
			}
			return nil
		default:
			return event
		}
	})
	//
	renameWindow = tview.NewInputField().
		SetLabel("Enter a msg index: ").
		SetFieldWidth(20).
		SetAcceptanceFunc(tview.InputFieldMaxLength(100)).
		SetDoneFunc(func(key tcell.Key) {
			pages.RemovePage(renamePage)
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
			if err := notifyUser("renamed", notification); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
		}
		return event
	})
	//
	searchField = tview.NewInputField().
		SetPlaceholder("Search... (Enter: search)").
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				term := searchField.GetText()
				if term == "" {
					// If the search term is empty, cancel the search
					hideSearchBar()
					searchResults = nil
					searchResultLengths = nil
					originalTextForSearch = ""
					textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
					colorText()
					return
				} else {
					performSearch(term)
					// Keep focus on textView after search
					app.SetFocus(textView)
					hideSearchBar()
				}
			}
		})
	searchField.SetBorder(true).SetTitle("Search")
	// Note: Initially hide the search field (handled by not showing it in the layout)
	//
	helpView = tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf(helpText, makeStatusLine())).
		SetDoneFunc(func(key tcell.Key) {
			pages.RemovePage(helpPage)
		})
	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			return event
		}
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(helpPage)
			return nil
		}
		// Allow scrolling keys to pass through to the TextView
		switch event.Key() {
		case tcell.KeyUp, tcell.KeyDown,
			tcell.KeyPgUp, tcell.KeyPgDn,
			tcell.KeyHome, tcell.KeyEnd:
			return event
		}
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'j', 'k', 'g', 'G':
				return event
			}
		}
		return nil
	})
	//
	imgView = tview.NewImage()
	imgView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			pages.RemovePage(imgPage)
			return event
		}
		if isASCII(string(event.Rune())) {
			pages.RemovePage(imgPage)
			return event
		}
		return nil
	})
	//
	textArea.SetMovedFunc(updateStatusLine)
	updateStatusLine()
	textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
	colorText()
	if scrollToEndEnabled {
		textView.ScrollToEnd()
	}
	// init sysmap
	_, err := initSysCards()
	if err != nil {
		logger.Error("failed to init sys cards", "error", err)
	}
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == '5' && event.Modifiers()&tcell.ModAlt != 0 {
			// switch cfg.ShowSys
			cfg.ShowSys = !cfg.ShowSys
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			colorText()
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '3' && event.Modifiers()&tcell.ModAlt != 0 {
			go summarizeAndStartNewChat()
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '6' && event.Modifiers()&tcell.ModAlt != 0 {
			// toggle status line visibility
			if name, _ := pages.GetFrontPage(); name != "main" {
				return event
			}
			positionVisible = !positionVisible
			updateFlexLayout()
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '2' && event.Modifiers()&tcell.ModAlt != 0 {
			// toggle auto-scrolling
			scrollToEndEnabled = !scrollToEndEnabled
			status := "disabled"
			if scrollToEndEnabled {
				status = "enabled"
			}
			if err := notifyUser("autoscroll", "Auto-scrolling "+status); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			updateStatusLine()
		}
		// Handle Alt+7 to toggle injectRole
		if event.Key() == tcell.KeyRune && event.Rune() == '7' && event.Modifiers()&tcell.ModAlt != 0 {
			injectRole = !injectRole
			updateStatusLine()
		}
		// Handle Alt+T to toggle thinking block visibility
		if event.Key() == tcell.KeyRune && event.Rune() == 't' && event.Modifiers()&tcell.ModAlt != 0 {
			thinkingCollapsed = !thinkingCollapsed
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			colorText()
			status := "expanded"
			if thinkingCollapsed {
				status = "collapsed"
			}
			if err := notifyUser("thinking", "Thinking blocks "+status); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == 'i' && event.Modifiers()&tcell.ModAlt != 0 {
			if isFullScreenPageActive() {
				return event
			}
			showColorschemeSelectionPopup()
			return nil
		}
		if event.Key() == tcell.KeyF1 {
			// chatList, err := loadHistoryChats()
			chatList, err := store.GetChatByChar(cfg.AssistantRole)
			if err != nil {
				logger.Error("failed to load chat history", "error", err)
				return nil
			}
			// Check if there are no chats for this agent
			if len(chatList) == 0 {
				notification := "no chats found for agent: " + cfg.AssistantRole
				if err := notifyUser("info", notification); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				return nil
			}
			chatMap := make(map[string]models.Chat)
			// nameList := make([]string, len(chatList))
			for _, chat := range chatList {
				// nameList[i] = chat.Name
				chatMap[chat.Name] = chat
			}
			chatActTable := makeChatTable(chatMap)
			pages.AddPage(historyPage, chatActTable, true, true)
			colorText()
			updateStatusLine()
			return nil
		}
		if event.Key() == tcell.KeyF2 && !botRespMode {
			// regen last msg
			if len(chatBody.Messages) == 0 {
				if err := notifyUser("info", "no messages to regenerate"); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				return nil
			}
			chatBody.Messages = chatBody.Messages[:len(chatBody.Messages)-1]
			// there is no case where user msg is regenerated
			// lastRole := chatBody.Messages[len(chatBody.Messages)-1].Role
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			// go chatRound("", cfg.UserRole, textView, true, false)
			if cfg.TTS_ENABLED {
				TTSDoneChan <- true
			}
			chatRoundChan <- &models.ChatRoundReq{Role: cfg.UserRole, Regen: true}
			return nil
		}
		if event.Key() == tcell.KeyF3 && !botRespMode {
			// delete last msg
			// check textarea text; if it ends with bot icon delete only icon:
			text := textView.GetText(true)
			assistantIcon := roleToIcon(cfg.AssistantRole)
			if strings.HasSuffix(text, assistantIcon) {
				logger.Debug("deleting assistant icon", "icon", assistantIcon)
				textView.SetText(strings.TrimSuffix(text, assistantIcon))
				colorText()
				return nil
			}
			if len(chatBody.Messages) == 0 {
				if err := notifyUser("info", "no messages to delete"); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				return nil
			}
			chatBody.Messages = chatBody.Messages[:len(chatBody.Messages)-1]
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			if cfg.TTS_ENABLED {
				TTSDoneChan <- true
			}
			colorText()
			return nil
		}
		if event.Key() == tcell.KeyF4 {
			// edit msg - show index input as overlay at top
			editMode = true
			showIndexBar()
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Modifiers() == tcell.ModAlt && event.Rune() == '4' {
			// edit msg role - show index input as overlay at top
			editMode = false // Reset edit mode to false to handle role editing
			showIndexBar()
			// Set a flag to indicate we're in role edit mode
			roleEditMode = true
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			// toggle fullscreen
			fullscreenMode = !fullscreenMode
			focused := app.GetFocus()
			if fullscreenMode {
				if focused == textArea || focused == textView {
					flex.Clear()
					flex.AddItem(focused, 0, 1, true)
				} else {
					// if focus is not on textarea or textview, cancel fullscreen
					fullscreenMode = false
				}
			} else {
				// focused is the fullscreened widget here
				updateFlexLayout()
			}
			return nil
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
			if err := copyToClipboard(m.Content); err != nil {
				logger.Error("failed to copy to clipboard", "error", err)
			}
			previewLen := min(30, len(m.Content))
			notification := fmt.Sprintf("msg '%s' was copied to the clipboard", m.Content[:previewLen])
			if err := notifyUser("copied", notification); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			return nil
		}
		if event.Key() == tcell.KeyF8 {
			// copy msg to clipboard
			editMode = false
			showIndexBar()
			return nil
		}
		if event.Key() == tcell.KeyF9 {
			// table of codeblocks to copy
			text := textView.GetText(false)
			cb := codeBlockRE.FindAllString(text, -1)
			if len(cb) == 0 {
				if err := notifyUser("notify", "no code blocks in chat"); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				return nil
			}
			table := makeCodeBlockTable(cb)
			pages.AddPage(codeBlockPage, table, true, true)
			return nil
		}
		if event.Key() == tcell.KeyF10 {
			cfg.SkipLLMResp = !cfg.SkipLLMResp
			updateStatusLine()
		}
		if event.Key() == tcell.KeyF11 {
			// read files in chat_exports
			filelist, err := os.ReadDir(exportDir)
			if err != nil {
				if err := notifyUser("failed to load exports", err.Error()); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				return nil
			}
			fli := []string{}
			for _, f := range filelist {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
					continue
				}
				fpath := path.Join(exportDir, f.Name())
				fli = append(fli, fpath)
			}
			// check error
			exportsTable := makeImportChatTable(fli)
			pages.AddPage(historyPage, exportsTable, true, true)
			updateStatusLine()
			return nil
		}
		if event.Key() == tcell.KeyF12 {
			// help window cheatsheet
			// Update help text with current status before showing
			helpView.SetText(fmt.Sprintf(helpText, makeStatusLine()))
			pages.AddPage(helpPage, helpView, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlE {
			// export loaded chat into json file
			if err := exportChat(); err != nil {
				logger.Error("failed to export chat;", "error", err, "chat_name", activeChatName)
				return nil
			}
			if err := notifyUser("exported chat", "chat: "+activeChatName+" was exported"); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			return nil
		}
		if event.Key() == tcell.KeyCtrlP {
			propsTable := makePropsTable(defaultLCPProps)
			pages.AddPage(propsPage, propsTable, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlN {
			startNewChat(true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlO {
			// open file picker
			filePicker := makeFilePicker()
			pages.AddPage(filePickerPage, filePicker, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlL {
			if isFullScreenPageActive() {
				return event
			}
			// Show model selection popup instead of rotating models
			showModelSelectionPopup()
			return nil
		}
		if event.Key() == tcell.KeyCtrlT {
			// clear context
			// remove tools and thinking
			removeThinking(chatBody)
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			colorText()
			return nil
		}
		if event.Key() == tcell.KeyCtrlV {
			if isFullScreenPageActive() {
				return event
			}
			// Show API link selection popup instead of rotating APIs
			showAPILinkSelectionPopup()
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			// switch sys prompt
			labels, err := initSysCards()
			if err != nil {
				logger.Error("failed to read sys dir", "error", err)
				if err := notifyUser("error", "failed to read: "+cfg.SysDir); err != nil {
					logger.Debug("failed to notify user", "error", err)
				}
				return nil
			}
			at := makeAgentTable(labels)
			// sysModal.AddButtons(labels)
			// load all chars
			pages.AddPage(agentPage, at, true, true)
			updateStatusLine()
			return nil
		}
		if event.Key() == tcell.KeyCtrlK {
			// add message from tools
			cfg.ToolUse = !cfg.ToolUse
			updateStatusLine()
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '8' && event.Modifiers()&tcell.ModAlt != 0 {
			// show image - check for attached image first, then fall back to agent image
			if lastImg != "" {
				// Load the attached image
				file, err := os.Open(lastImg)
				if err != nil {
					logger.Error("failed to open attached image", "path", lastImg, "error", err)
					// Fall back to showing agent image
					loadImage()
				} else {
					defer file.Close()
					img, _, err := image.Decode(file)
					if err != nil {
						logger.Error("failed to decode attached image", "path", lastImg, "error", err)
						// Fall back to showing agent image
						loadImage()
					} else {
						imgView.SetImage(img)
					}
				}
			} else {
				// No attached image, show agent image as before
				loadImage()
			}
			pages.AddPage(imgPage, imgView, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlR && cfg.STT_ENABLED {
			defer updateStatusLine()
			if asr.IsRecording() {
				userSpeech, err := asr.StopRecording()
				if err != nil {
					msg := "failed to inference user speech; error:" + err.Error()
					logger.Error(msg)
					if err := notifyUser("stt error", msg); err != nil {
						logger.Error("failed to notify user", "error", err)
					}
					return nil
				}
				if userSpeech != "" {
					// append indtead of replacing
					prevText := textArea.GetText()
					textArea.SetText(prevText+userSpeech, true)
				} else {
					logger.Warn("empty user speech")
				}
				return nil
			}
			if err := asr.StartRecording(); err != nil {
				logger.Error("failed to start recording user speech", "error", err)
				return nil
			}
		}
		// I need keybind for tts to shut up
		if event.Key() == tcell.KeyCtrlA && cfg.TTS_ENABLED {
			TTSDoneChan <- true
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '0' && event.Modifiers()&tcell.ModAlt != 0 && cfg.TTS_ENABLED {
			if len(chatBody.Messages) > 0 {
				// Stop any currently playing TTS first
				TTSDoneChan <- true
				lastMsg := chatBody.Messages[len(chatBody.Messages)-1]
				cleanedText := models.CleanText(lastMsg.Content)
				if cleanedText != "" {
					// nolint: errcheck
					go orator.Speak(cleanedText)
				}
			}
			return nil
		}
		if event.Key() == tcell.KeyCtrlW {
			// INFO: continue bot/text message
			// without new role
			lastRole := chatBody.Messages[len(chatBody.Messages)-1].Role
			// go chatRound("", lastRole, textView, false, true)
			chatRoundChan <- &models.ChatRoundReq{Role: lastRole, Resume: true}
			return nil
		}
		if event.Key() == tcell.KeyCtrlQ {
			if isFullScreenPageActive() {
				return event
			}
			// Show user role selection popup instead of cycling through roles
			showUserRoleSelectionPopup()
			return nil
		}
		if event.Key() == tcell.KeyCtrlX {
			if isFullScreenPageActive() {
				return event
			}
			// Show bot role selection popup instead of cycling through roles
			showBotRoleSelectionPopup()
			return nil
		}
		if event.Key() == tcell.KeyCtrlG {
			// cfg.RAGDir is the directory with files to use with RAG
			// rag load
			// menu of the text files from defined rag directory
			files, err := os.ReadDir(cfg.RAGDir)
			if err != nil {
				// Check if the error is because the directory doesn't exist
				if os.IsNotExist(err) {
					// Create the RAG directory if it doesn't exist
					if mkdirErr := os.MkdirAll(cfg.RAGDir, 0755); mkdirErr != nil {
						logger.Error("failed to create RAG directory", "dir", cfg.RAGDir, "error", mkdirErr)
						if notifyerr := notifyUser("failed to create RAG directory", mkdirErr.Error()); notifyerr != nil {
							logger.Error("failed to send notification", "error", notifyerr)
						}
						return nil
					}
					// Now try to read the directory again after creating it
					files, err = os.ReadDir(cfg.RAGDir)
					if err != nil {
						logger.Error("failed to read dir after creating it", "dir", cfg.RAGDir, "error", err)
						if notifyerr := notifyUser("failed to read RAG directory", err.Error()); notifyerr != nil {
							logger.Error("failed to send notification", "error", notifyerr)
						}
						return nil
					}
				} else {
					// Other error (permissions, etc.)
					logger.Error("failed to read dir", "dir", cfg.RAGDir, "error", err)
					if notifyerr := notifyUser("failed to open RAG files dir", err.Error()); notifyerr != nil {
						logger.Error("failed to send notification", "error", notifyerr)
					}
					return nil
				}
			}
			fileList := []string{}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				fileList = append(fileList, f.Name())
			}
			chatRAGTable := makeRAGTable(fileList)
			pages.AddPage(RAGPage, chatRAGTable, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlY { // Use Ctrl+Y to list loaded RAG files
			// List files already loaded into the RAG system
			fileList, err := ragger.ListLoaded()
			if err != nil {
				logger.Error("failed to list loaded RAG files", "error", err)
				if notifyerr := notifyUser("failed to list RAG files", err.Error()); notifyerr != nil {
					logger.Error("failed to send notification", "error", notifyerr)
				}
				return nil
			}
			chatLoadedRAGTable := makeLoadedRAGTable(fileList)
			pages.AddPage(RAGLoadedPage, chatLoadedRAGTable, true, true)
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Modifiers() == tcell.ModAlt && event.Rune() == '1' {
			// Toggle shell mode: when enabled, commands are executed locally instead of sent to LLM
			toggleShellMode()
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Modifiers() == tcell.ModAlt && event.Rune() == '9' {
			// Warm up (load) the currently selected model
			go warmUpModel()
			if err := notifyUser("model warmup", "loading model: "+chatBody.Model); err != nil {
				logger.Debug("failed to notify user", "error", err)
			}
			return nil
		}
		// cannot send msg in editMode or botRespMode
		if event.Key() == tcell.KeyEscape && !editMode && !botRespMode {
			msgText := textArea.GetText()
			if shellMode && msgText != "" {
				// In shell mode, execute command instead of sending to LLM
				executeCommandAndDisplay(msgText)
				textArea.SetText("", true) // Clear the input area
				return nil
			} else if !shellMode {
				// Normal mode - send to LLM
				nl := "\n\n" // keep empty lines between messages
				prevText := textView.GetText(true)
				persona := cfg.UserRole
				// strings.LastIndex()
				// newline is not needed is prev msg ends with one
				if strings.HasSuffix(prevText, nl) {
					nl = ""
				} else if strings.HasSuffix(prevText, "\n") {
					nl = "\n" // only one newline, add another
				}
				if msgText != "" {
					// as what char user sends msg?
					if cfg.WriteNextMsgAs != "" {
						persona = cfg.WriteNextMsgAs
					}
					// check if plain text
					if !injectRole {
						matches := roleRE.FindStringSubmatch(msgText)
						if len(matches) > 1 {
							persona = matches[1]
							msgText = strings.TrimLeft(msgText[len(matches[0]):], " ")
						}
					}
					// add user icon before user msg
					fmt.Fprintf(textView, "%s[-:-:b](%d) <%s>: [-:-:-]\n%s\n",
						nl, len(chatBody.Messages), persona, msgText)
					textArea.SetText("", true)
					if scrollToEndEnabled {
						textView.ScrollToEnd()
					}
					colorText()
				}
				// go chatRound(msgText, persona, textView, false, false)
				chatRoundChan <- &models.ChatRoundReq{Role: persona, UserMsg: msgText}
			}
			return nil
		}
		if event.Key() == tcell.KeyPgUp || event.Key() == tcell.KeyPgDn {
			currentF := app.GetFocus()
			app.SetFocus(focusSwitcher[currentF])
			return nil
		}
		if isASCII(string(event.Rune())) && !botRespMode {
			return event
		}
		return event
	})
}
