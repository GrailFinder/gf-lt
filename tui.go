package main

import (
	"elefant/models"
	"elefant/pngmeta"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
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
	// pages
	historyPage   = "historyPage"
	agentPage     = "agentPage"
	editMsgPage   = "editMsgPage"
	indexPage     = "indexPage"
	helpPage      = "helpPage"
	renamePage    = "renamePage"
	RAGPage       = "RAGPage "
	propsPage     = "propsPage"
	codeBlockPage = "codeBlockPage"
	imgPage       = "imgPage"
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
[yellow]F10[white]: manage loaded rag files (that already in vector db)
[yellow]F11[white]: switch RAGEnabled boolean
[yellow]F12[white]: show this help page
[yellow]Ctrl+w[white]: resume generation on the last msg
[yellow]Ctrl+s[white]: load new char/agent
[yellow]Ctrl+e[white]: export chat to json file
[yellow]Ctrl+n[white]: start a new chat
[yellow]Ctrl+c[white]: close programm
[yellow]Ctrl+p[white]: props edit form (min-p, dry, etc.)
[yellow]Ctrl+v[white]: switch between /completion and /chat api (if provided in config)
[yellow]Ctrl+r[white]: menu of files that can be loaded in vector db (RAG)
[yellow]Ctrl+t[white]: remove thinking (<think>) and tool messages from context (delete from chat)
[yellow]Ctrl+l[white]: update connected model name (llamacpp)
[yellow]Ctrl+k[white]: switch tool use (recommend tool use to llm after user msg)
[yellow]Ctrl+j[white]: if chat agent is char.png will show the image; then any key to return

Press Enter to go back
`
)

func loadImage() {
	filepath := defaultImage
	cc, ok := sysMap[cfg.AssistantRole]
	if ok {
		if strings.HasSuffix(cc.FilePath, ".png") {
			filepath = cc.FilePath
		}
	}
	file, err := os.Open(filepath)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		panic(err)
	}
	imgView.SetImage(img)
}

func colorText() {
	text := textView.GetText(false)
	// Step 1: Extract code blocks and replace them with unique placeholders
	var codeBlocks []string
	placeholder := "__CODE_BLOCK_%d__"
	counter := 0
	// thinking
	var thinkBlocks []string
	placeholderThink := "__THINK_BLOCK_%d__"
	counterThink := 0
	// Replace code blocks with placeholders and store their styled versions
	text = codeBlockRE.ReplaceAllStringFunc(text, func(match string) string {
		// Style the code block and store it
		styled := fmt.Sprintf("[red::i]%s[-:-:-]", match)
		codeBlocks = append(codeBlocks, styled)
		// Generate a unique placeholder (e.g., "__CODE_BLOCK_0__")
		id := fmt.Sprintf(placeholder, counter)
		counter++
		return id
	})
	text = thinkRE.ReplaceAllStringFunc(text, func(match string) string {
		// Style the code block and store it
		styled := fmt.Sprintf("[red::i]%s[-:-:-]", match)
		thinkBlocks = append(thinkBlocks, styled)
		// Generate a unique placeholder (e.g., "__CODE_BLOCK_0__")
		id := fmt.Sprintf(placeholderThink, counterThink)
		counterThink++
		return id
	})
	// Step 2: Apply other regex styles to the non-code parts
	text = quotesRE.ReplaceAllString(text, `[orange::-]$1[-:-:-]`)
	text = starRE.ReplaceAllString(text, `[turquoise::i]$1[-:-:-]`)
	// text = thinkRE.ReplaceAllString(text, `[yellow::i]$1[-:-:-]`)
	// Step 3: Restore the styled code blocks from placeholders
	for i, cb := range codeBlocks {
		text = strings.Replace(text, fmt.Sprintf(placeholder, i), cb, 1)
	}
	logger.Debug("thinking debug", "blocks", thinkBlocks)
	for i, tb := range thinkBlocks {
		text = strings.Replace(text, fmt.Sprintf(placeholderThink, i), tb, 1)
	}
	textView.SetText(text)
}

func updateStatusLine() {
	position.SetText(fmt.Sprintf(indexLine, botRespMode, cfg.AssistantRole, activeChatName, cfg.RAGEnabled, cfg.ToolUse, chatBody.Model, cfg.CurrentAPI, cfg.ThinkUse, logLevel.Level()))
}

func initSysCards() ([]string, error) {
	labels := []string{}
	labels = append(labels, sysLabels...)
	cards, err := pngmeta.ReadDirCards(cfg.SysDir, cfg.UserRole, logger)
	if err != nil {
		logger.Error("failed to read sys dir", "error", err)
		return nil, err
	}
	for _, cc := range cards {
		if cc.Role == "" {
			logger.Warn("empty role", "file", cc.FilePath)
			continue
		}
		sysMap[cc.Role] = cc
		labels = append(labels, cc.Role)
	}
	return labels, nil
}

func startNewChat() {
	id, err := store.ChatGetMaxID()
	if err != nil {
		logger.Error("failed to get chat id", "error", err)
	}
	if ok := charToStart(cfg.AssistantRole); !ok {
		logger.Warn("no such sys msg", "name", cfg.AssistantRole)
	}
	// set chat body
	chatBody.Messages = chatBody.Messages[:2]
	textView.SetText(chatToText(cfg.ShowSys))
	newChat := &models.Chat{
		ID:    id + 1,
		Name:  fmt.Sprintf("%d_%s", id+1, cfg.AssistantRole),
		Msgs:  string(defaultStarterBytes),
		Agent: cfg.AssistantRole,
	}
	activeChatName = newChat.Name
	chatMap[newChat.Name] = newChat
	updateStatusLine()
	colorText()
}

func setLogLevel(sl string) {
	switch sl {
	case "Debug":
		logLevel.Set(-4)
	case "Info":
		logLevel.Set(0)
	case "Warn":
		logLevel.Set(4)
	}
}

func makePropsForm(props map[string]float32) *tview.Form {
	// https://github.com/rivo/tview/commit/0a18dea458148770d212d348f656988df75ff341
	// no way to close a form by a key press; a shame.
	form := tview.NewForm().
		AddTextView("Notes", "Props for llamacpp completion call", 40, 2, true, false).
		AddCheckbox("Insert <think> (/completion only)", cfg.ThinkUse, func(checked bool) {
			cfg.ThinkUse = checked
		}).AddDropDown("Set log level (Enter): ", []string{"Debug", "Info", "Warn"}, 1,
		func(option string, optionIndex int) {
			setLogLevel(option)
		}).AddDropDown("Select an api: ", slices.Insert(cfg.ApiLinks, 0, cfg.CurrentAPI), 0,
		func(option string, optionIndex int) {
			cfg.CurrentAPI = option
		}).AddDropDown("Select a model: ", []string{chatBody.Model, "deepseek-chat", "deepseek-reasoner"}, 0,
		func(option string, optionIndex int) {
			chatBody.Model = option
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
	theme := tview.Theme{
		PrimitiveBackgroundColor:    tcell.ColorDefault,
		ContrastBackgroundColor:     tcell.ColorGray,
		MoreContrastBackgroundColor: tcell.ColorNavy,
		BorderColor:                 tcell.ColorGray,
		TitleColor:                  tcell.ColorRed,
		GraphicsColor:               tcell.ColorBlue,
		PrimaryTextColor:            tcell.ColorLightGray,
		SecondaryTextColor:          tcell.ColorYellow,
		TertiaryTextColor:           tcell.ColorOrange,
		InverseTextColor:            tcell.ColorPurple,
		ContrastSecondaryTextColor:  tcell.ColorLime,
	}
	tview.Styles = theme
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
		AddItem(textArea, 0, 10, true).
		AddItem(position, 0, 2, false)
	editArea = tview.NewTextArea().
		SetPlaceholder("Replace msg...")
	editArea.SetBorder(true).SetTitle("input")
	editArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		defer func() {
			editMode = false
		}()
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
				if err := notifyUser("cancel", "no index provided"); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				pages.RemovePage(indexPage)
				return event
			}
			selectedIndex = siInt
			if len(chatBody.Messages)+1 < selectedIndex || selectedIndex < 0 {
				msg := "chosen index is out of bounds"
				logger.Warn(msg, "index", selectedIndex)
				if err := notifyUser("error", msg); err != nil {
					logger.Error("failed to send notification", "error", err)
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
				previewLen := 30
				if len(m.Content) < 30 {
					previewLen = len(m.Content)
				}
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
	helpView = tview.NewTextView().SetDynamicColors(true).SetText(helpText).SetDoneFunc(func(key tcell.Key) {
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
			previewLen := 30
			if len(m.Content) < 30 {
				previewLen = len(m.Content)
			}
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
			// updateStatusLine()
			return nil
		}
		if event.Key() == tcell.KeyF10 {
			// list rag loaded in db
			loadedFiles, err := ragger.ListLoaded()
			if err != nil {
				logger.Error("failed to list regfiles in db", "error", err)
				return nil
			}
			if len(loadedFiles) == 0 {
				if err := notifyUser("loaded RAG", "no files in db"); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
				return nil
			}
			dbRAGTable := makeLoadedRAGTable(loadedFiles)
			pages.AddPage(RAGPage, dbRAGTable, true, true)
			return nil
		}
		if event.Key() == tcell.KeyF11 {
			// xor
			cfg.RAGEnabled = !cfg.RAGEnabled
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
		if event.Key() == tcell.KeyCtrlL {
			go func() {
				fetchModelName() // blocks
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
			// switch between /chat and /completion api
			newAPI := cfg.APIMap[cfg.CurrentAPI]
			if newAPI == "" {
				// do not switch
				return nil
			}
			cfg.CurrentAPI = newAPI
			if strings.Contains(cfg.CurrentAPI, "deepseek") {
				chatBody.Model = "deepseek-chat"
			} else {
				chatBody.Model = "local"
			}
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
			// show image
			loadImage()
			pages.AddPage(imgPage, imgView, true, true)
			return nil
		}
		if event.Key() == tcell.KeyCtrlR && cfg.HFToken != "" {
			// rag load
			// menu of the text files from defined rag directory
			files, err := os.ReadDir(cfg.RAGDir)
			if err != nil {
				logger.Error("failed to read dir", "dir", cfg.RAGDir, "error", err)
				return nil
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
		if event.Key() == tcell.KeyCtrlW {
			// INFO: continue bot/text message
			// without new role
			lastRole := chatBody.Messages[len(chatBody.Messages)-1].Role
			go chatRound("", lastRole, textView, false, true)
			return nil
		}
		// cannot send msg in editMode or botRespMode
		if event.Key() == tcell.KeyEscape && !editMode && !botRespMode {
			// read all text into buffer
			msgText := textArea.GetText()
			nl := "\n"
			prevText := textView.GetText(true)
			// strings.LastIndex()
			// newline is not needed is prev msg ends with one
			if strings.HasSuffix(prevText, nl) {
				nl = ""
			}
			if msgText != "" {
				// add user icon before user msg
				fmt.Fprintf(textView, "%s[-:-:b](%d) <%s>: [-:-:-]\n%s\n",
					nl, len(chatBody.Messages), cfg.UserRole, msgText)
				textArea.SetText("", true)
				textView.ScrollToEnd()
				colorText()
			}
			// update statue line
			go chatRound(msgText, cfg.UserRole, textView, false, false)
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
