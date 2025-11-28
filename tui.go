package main

import (
	"fmt"
	"gf-lt/extra"
	"gf-lt/models"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

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
	imgView         *tview.Image
	defaultImage    = "sysprompts/llama.png"
	indexPickWindow *tview.InputField
	renameWindow    *tview.InputField
	fullscreenMode  bool
	// pages
	historyPage    = "historyPage"
	agentPage      = "agentPage"
	editMsgPage    = "editMsgPage"
	indexPage      = "indexPage"
	helpPage       = "helpPage"
	renamePage     = "renamePage"
	RAGPage        = "RAGPage"
	RAGLoadedPage  = "RAGLoadedPage"
	propsPage      = "propsPage"
	codeBlockPage  = "codeBlockPage"
	imgPage        = "imgPage"
	filePickerPage = "filePicker"
	exportDir      = "chat_exports"
	// help text
	helpText = `
[yellow]Esc[white]: send msg
[yellow]PgUp/Down[white]: switch focus between input and chat widgets
[yellow]F1[white]: manage chats
[yellow]F2[white]: regen last
[yellow]F3[white]: delete last msg
[yellow]F4[white]: edit msg
[yellow]F5[white]: toggle system
[yellow]F6[white]: interrupt bot resp
[yellow]F7[white]: copy last msg to clipboard (linux xclip)
[yellow]F8[white]: copy n msg to clipboard (linux xclip)
[yellow]F9[white]: table to copy from; with all code blocks
[yellow]F10[white]: switch if LLM will respond on this message (for user to write multiple messages in a row)
[yellow]F11[white]: import chat file
[yellow]F12[white]: show this help page
[yellow]Ctrl+w[white]: resume generation on the last msg
[yellow]Ctrl+s[white]: load new char/agent
[yellow]Ctrl+e[white]: export chat to json file
[yellow]Ctrl+c[white]: close programm
[yellow]Ctrl+n[white]: start a new chat
[yellow]Ctrl+o[white]: open image file picker
[yellow]Ctrl+p[white]: props edit form (min-p, dry, etc.)
[yellow]Ctrl+v[white]: switch between /completion and /chat api (if provided in config)
[yellow]Ctrl+r[white]: start/stop recording from your microphone (needs stt server)
[yellow]Ctrl+t[white]: remove thinking (<think>) and tool messages from context (delete from chat)
[yellow]Ctrl+l[white]: update connected model name (llamacpp)
[yellow]Ctrl+k[white]: switch tool use (recommend tool use to llm after user msg)
[yellow]Ctrl+j[white]: if chat agent is char.png will show the image; then any key to return
[yellow]Ctrl+a[white]: interrupt tts (needs tts server)
[yellow]Ctrl+g[white]: open RAG file manager (load files for context retrieval)
[yellow]Ctrl+y[white]: list loaded RAG files (view and manage loaded files)
[yellow]Ctrl+q[white]: cycle through mentioned chars in chat, to pick persona to send next msg as
[yellow]Ctrl+x[white]: cycle through mentioned chars in chat, to pick persona to send next msg as (for llm)
[yellow]Alt+5[white]: toggle fullscreen for input/chat window

%s

Press Enter to go back
`
	colorschemes = map[string]tview.Theme{
		"default": tview.Theme{
			PrimitiveBackgroundColor:    tcell.ColorDefault,
			ContrastBackgroundColor:     tcell.ColorGray,
			MoreContrastBackgroundColor: tcell.ColorSteelBlue,
			BorderColor:                 tcell.ColorGray,
			TitleColor:                  tcell.ColorRed,
			GraphicsColor:               tcell.ColorBlue,
			PrimaryTextColor:            tcell.ColorLightGray,
			SecondaryTextColor:          tcell.ColorYellow,
			TertiaryTextColor:           tcell.ColorOrange,
			InverseTextColor:            tcell.ColorPurple,
			ContrastSecondaryTextColor:  tcell.ColorLime,
		},
		"gruvbox": tview.Theme{
			PrimitiveBackgroundColor:    tcell.ColorBlack,         // Matches #1e1e2e
			ContrastBackgroundColor:     tcell.ColorDarkGoldenrod, // Selected option: warm yellow (#b57614)
			MoreContrastBackgroundColor: tcell.ColorDarkSlateGray, // Non-selected options: dark grayish-blue (#32302f)
			BorderColor:                 tcell.ColorLightGray,     // Light gray (#a89984)
			TitleColor:                  tcell.ColorRed,           // Red (#fb4934)
			GraphicsColor:               tcell.ColorDarkCyan,      // Cyan (#689d6a)
			PrimaryTextColor:            tcell.ColorLightGray,     // Light gray (#d5c4a1)
			SecondaryTextColor:          tcell.ColorYellow,        // Yellow (#fabd2f)
			TertiaryTextColor:           tcell.ColorOrange,        // Orange (#fe8019)
			InverseTextColor:            tcell.ColorWhite,         // White (#f9f5d7) for selected text
			ContrastSecondaryTextColor:  tcell.ColorLightGreen,    // Light green (#b8bb26)
		},
		"solarized": tview.Theme{
			PrimitiveBackgroundColor:    tcell.NewHexColor(0x1e1e2e), // #1e1e2e for main dropdown box
			ContrastBackgroundColor:     tcell.ColorDarkCyan,         // Selected option: cyan (#2aa198)
			MoreContrastBackgroundColor: tcell.ColorDarkSlateGray,    // Non-selected options: dark blue (#073642)
			BorderColor:                 tcell.ColorLightBlue,        // Light blue (#839496)
			TitleColor:                  tcell.ColorRed,              // Red (#dc322f)
			GraphicsColor:               tcell.ColorBlue,             // Blue (#268bd2)
			PrimaryTextColor:            tcell.ColorWhite,            // White (#fdf6e3)
			SecondaryTextColor:          tcell.ColorYellow,           // Yellow (#b58900)
			TertiaryTextColor:           tcell.ColorOrange,           // Orange (#cb4b16)
			InverseTextColor:            tcell.ColorWhite,            // White (#eee8d5) for selected text
			ContrastSecondaryTextColor:  tcell.ColorLightCyan,        // Light cyan (#93a1a1)
		},
		"dracula": tview.Theme{
			PrimitiveBackgroundColor:    tcell.NewHexColor(0x1e1e2e), // #1e1e2e for main dropdown box
			ContrastBackgroundColor:     tcell.ColorDarkMagenta,      // Selected option: magenta (#bd93f9)
			MoreContrastBackgroundColor: tcell.ColorDarkGray,         // Non-selected options: dark gray (#44475a)
			BorderColor:                 tcell.ColorLightGray,        // Light gray (#f8f8f2)
			TitleColor:                  tcell.ColorRed,              // Red (#ff5555)
			GraphicsColor:               tcell.ColorDarkCyan,         // Cyan (#8be9fd)
			PrimaryTextColor:            tcell.ColorWhite,            // White (#f8f8f2)
			SecondaryTextColor:          tcell.ColorYellow,           // Yellow (#f1fa8c)
			TertiaryTextColor:           tcell.ColorOrange,           // Orange (#ffb86c)
			InverseTextColor:            tcell.ColorWhite,            // White (#f8f8f2) for selected text
			ContrastSecondaryTextColor:  tcell.ColorLightGreen,       // Light green (#50fa7b)
		},
	}
)

func makePropsForm(props map[string]float32) *tview.Form {
	// https://github.com/rivo/tview/commit/0a18dea458148770d212d348f656988df75ff341
	// no way to close a form by a key press; a shame.
	modelList := []string{chatBody.Model, "deepseek-chat", "deepseek-reasoner"}
	modelList = append(modelList, ORFreeModels...)
	form := tview.NewForm().
		AddTextView("Notes", "Props for llamacpp completion call", 40, 2, true, false).
		AddCheckbox("Insert <think> (/completion only)", cfg.ThinkUse, func(checked bool) {
			cfg.ThinkUse = checked
		}).AddCheckbox("RAG use", cfg.RAGEnabled, func(checked bool) {
		cfg.RAGEnabled = checked
	}).AddCheckbox("Inject role", injectRole, func(checked bool) {
		injectRole = checked
	}).AddDropDown("Set log level (Enter): ", []string{"Debug", "Info", "Warn"}, 1,
		func(option string, optionIndex int) {
			setLogLevel(option)
		}).AddDropDown("Select an api: ", slices.Insert(cfg.ApiLinks, 0, cfg.CurrentAPI), 0,
		func(option string, optionIndex int) {
			cfg.CurrentAPI = option
		}).AddDropDown("Select a model: ", modelList, 0,
		func(option string, optionIndex int) {
			chatBody.Model = option
		}).AddDropDown("Write next message as: ", listRolesWithUser(), 0,
		func(option string, optionIndex int) {
			cfg.WriteNextMsgAs = option
		}).AddInputField("new char to write msg as: ", "", 32, tview.InputFieldMaxLength(32),
		func(text string) {
			if text != "" {
				cfg.WriteNextMsgAs = text
			}
		}).AddInputField("username: ", cfg.UserRole, 32, tview.InputFieldMaxLength(32), func(text string) {
		if text != "" {
			renameUser(cfg.UserRole, text)
			cfg.UserRole = text
		}
	}).
		AddButton("Quit", func() {
			pages.RemovePage(propsPage)
		})
	form.AddButton("Save", func() {
		defer updateStatusLine()
		defer pages.RemovePage(propsPage)
		for pn := range props {
			propField, ok := form.GetFormItemByLabel(pn).(*tview.InputField)
			if !ok {
				logger.Warn("failed to convert to inputfield", "prop_name", pn)
				continue
			}
			val, err := strconv.ParseFloat(propField.GetText(), 32)
			if err != nil {
				logger.Warn("failed parse to float", "value", propField.GetText())
				continue
			}
			props[pn] = float32(val)
		}
	})
	for propName, value := range props {
		form.AddInputField(propName, fmt.Sprintf("%v", value), 20, tview.InputFieldFloat, nil)
	}
	form.SetBorder(true).SetTitle("Enter some data").SetTitleAlign(tview.AlignLeft)
	return form
}

func init() {
	tview.Styles = colorschemes["default"]
	app = tview.NewApplication()
	pages = tview.NewPages()
	textArea = tview.NewTextArea().
		SetPlaceholder("input is multiline; press <Enter> to start the next line;\npress <Esc> to send the message.")
	textArea.SetBorder(true).SetTitle("input")
	textView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			app.Draw()
		})
	// textView.SetBorder(true).SetTitle("chat")
	textView.SetDoneFunc(func(key tcell.Key) {
		currentSelection := textView.GetHighlights()
		if key == tcell.KeyEnter {
			if len(currentSelection) > 0 {
				textView.Highlight()
			} else {
				textView.Highlight("0").ScrollToHighlight()
			}
		}
	})
	focusSwitcher[textArea] = textView
	focusSwitcher[textView] = textArea
	position = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	position.SetChangedFunc(func() {
		app.Draw()
	})
	flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textView, 0, 40, false).
		AddItem(textArea, 0, 10, true). // Restore original height
		AddItem(position, 0, 2, false)
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
			textView.SetText(chatToText(cfg.ShowSys))
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
			defer indexPickWindow.SetText("")
			pages.RemovePage(indexPage)
			// colorText()
			// updateStatusLine()
		})
	indexPickWindow.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyBackspace:
			return event
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
				pages.RemovePage(indexPage)
				return event
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
				pages.RemovePage(indexPage)
				return event
			}
			m := chatBody.Messages[selectedIndex]
			if editMode && event.Key() == tcell.KeyEnter {
				pages.RemovePage(indexPage)
				pages.AddPage(editMsgPage, editArea, true, true)
				editArea.SetText(m.Content, true)
			}
			if !editMode && event.Key() == tcell.KeyEnter {
				if err := copyToClipboard(m.Content); err != nil {
					logger.Error("failed to copy to clipboard", "error", err)
				}
				previewLen := min(30, len(m.Content))
				notification := fmt.Sprintf("msg '%s' was copied to the clipboard", m.Content[:previewLen])
				if err := notifyUser("copied", notification); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
			}
			return event
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
	helpView = tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf(helpText, makeStatusLine())).
		SetDoneFunc(func(key tcell.Key) {
			pages.RemovePage(helpPage)
		})
	helpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyEnter:
			return event
		}
		return nil
	})
	//
	imgView = tview.NewImage()
	imgView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
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
	textView.SetText(chatToText(cfg.ShowSys))
	colorText()
	textView.ScrollToEnd()
	// init sysmap
	_, err := initSysCards()
	if err != nil {
		logger.Error("failed to init sys cards", "error", err)
	}
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == '5' && event.Modifiers()&tcell.ModAlt != 0 {
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
				flex.Clear().
					AddItem(textView, 0, 40, false).
					AddItem(textArea, 0, 10, false).
					AddItem(position, 0, 2, false)

				if focused == textView {
					app.SetFocus(textView)
				} else { // default to textArea
					app.SetFocus(textArea)
				}
			}
			return nil
		}
		if event.Key() == tcell.KeyF1 {
			// chatList, err := loadHistoryChats()
			chatList, err := store.GetChatByChar(cfg.AssistantRole)
			if err != nil {
				logger.Error("failed to load chat history", "error", err)
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
		if event.Key() == tcell.KeyF2 {
			// regen last msg
			chatBody.Messages = chatBody.Messages[:len(chatBody.Messages)-1]
			// there is no case where user msg is regenerated
			// lastRole := chatBody.Messages[len(chatBody.Messages)-1].Role
			textView.SetText(chatToText(cfg.ShowSys))
			go chatRound("", cfg.UserRole, textView, true, false)
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
			chatBody.Messages = chatBody.Messages[:len(chatBody.Messages)-1]
			textView.SetText(chatToText(cfg.ShowSys))
			colorText()
			return nil
		}
		if event.Key() == tcell.KeyF4 {
			// edit msg
			editMode = true
			pages.AddPage(indexPage, indexPickWindow, true, true)
			return nil
		}
		if event.Key() == tcell.KeyF5 {
			// switch cfg.ShowSys
			cfg.ShowSys = !cfg.ShowSys
			textView.SetText(chatToText(cfg.ShowSys))
			colorText()
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
			pages.AddPage(indexPage, indexPickWindow, true, true)
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
			propsForm := makePropsForm(defaultLCPProps)
			pages.AddPage(propsPage, propsForm, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlN {
			startNewChat()
			return nil
		}
		if event.Key() == tcell.KeyCtrlO {
			// open file picker
			filePicker := makeFilePicker()
			pages.AddPage(filePickerPage, filePicker, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlL {
			go func() {
				fetchLCPModelName() // blocks
				updateStatusLine()
			}()
			return nil
		}
		if event.Key() == tcell.KeyCtrlT {
			// clear context
			// remove tools and thinking
			removeThinking(chatBody)
			textView.SetText(chatToText(cfg.ShowSys))
			colorText()
			return nil
		}
		if event.Key() == tcell.KeyCtrlV {
			// switch between API links using index-based rotation
			if len(cfg.ApiLinks) == 0 {
				// No API links to rotate through
				return nil
			}
			// Find current API in the list to get the current index
			currentIndex := -1
			for i, api := range cfg.ApiLinks {
				if api == cfg.CurrentAPI {
					currentIndex = i
					break
				}
			}
			// If current API is not in the list, start from beginning
			// Otherwise, advance to next API in the list (with wrap-around)
			if currentIndex == -1 {
				currentAPIIndex = 0
			} else {
				currentAPIIndex = (currentIndex + 1) % len(cfg.ApiLinks)
			}
			cfg.CurrentAPI = cfg.ApiLinks[currentAPIIndex]
			choseChunkParser()
			updateStatusLine()
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
		if event.Key() == tcell.KeyCtrlJ {
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
		if event.Key() == tcell.KeyCtrlA {
			// textArea.SetText("pressed ctrl+A", true)
			if cfg.TTS_ENABLED {
				// audioStream.TextChan <- chunk
				extra.TTSDoneChan <- true
			}
		}
		if event.Key() == tcell.KeyCtrlW {
			// INFO: continue bot/text message
			// without new role
			lastRole := chatBody.Messages[len(chatBody.Messages)-1].Role
			go chatRound("", lastRole, textView, false, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlQ {
			persona := cfg.UserRole
			if cfg.WriteNextMsgAs != "" {
				persona = cfg.WriteNextMsgAs
			}
			roles := listRolesWithUser()
			logger.Info("list roles", "roles", roles)
			for i, role := range roles {
				if strings.EqualFold(role, persona) {
					if i == len(roles)-1 {
						cfg.WriteNextMsgAs = roles[0] // reached last, get first
						break
					}
					cfg.WriteNextMsgAs = roles[i+1] // get next role
					logger.Info("picked role", "roles", roles, "index", i+1)
					break
				}
			}
			updateStatusLine()
			return nil
		}
		if event.Key() == tcell.KeyCtrlX {
			persona := cfg.AssistantRole
			if cfg.WriteNextMsgAsCompletionAgent != "" {
				persona = cfg.WriteNextMsgAsCompletionAgent
			}
			roles := chatBody.ListRoles()
			if len(roles) == 0 {
				logger.Warn("empty roles in chat")
			}
			if !strInSlice(cfg.AssistantRole, roles) {
				roles = append(roles, cfg.AssistantRole)
			}
			for i, role := range roles {
				if strings.EqualFold(role, persona) {
					if i == len(roles)-1 {
						cfg.WriteNextMsgAsCompletionAgent = roles[0] // reached last, get first
						break
					}
					cfg.WriteNextMsgAsCompletionAgent = roles[i+1] // get next role
					logger.Info("picked role", "roles", roles, "index", i+1)
					break
				}
			}
			updateStatusLine()
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
		// cannot send msg in editMode or botRespMode
		if event.Key() == tcell.KeyEscape && !editMode && !botRespMode {
			// read all text into buffer
			msgText := textArea.GetText()
			nl := "\n"
			prevText := textView.GetText(true)
			persona := cfg.UserRole
			// strings.LastIndex()
			// newline is not needed is prev msg ends with one
			if strings.HasSuffix(prevText, nl) {
				nl = ""
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
				textView.ScrollToEnd()
				colorText()
			}
			go chatRound(msgText, persona, textView, false, false)
			// Also clear any image attachment after sending the message
			go func() {
				// Wait a short moment for the message to be processed, then clear the image attachment
				// This allows the image to be sent with the current message if it was attached
				// But clears it for the next message
				ClearImageAttachment()
			}()
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
