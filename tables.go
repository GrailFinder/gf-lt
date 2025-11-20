package main

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"gf-lt/models"
	"gf-lt/pngmeta"
	"gf-lt/rag"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func makeChatTable(chatMap map[string]models.Chat) *tview.Table {
	actions := []string{"load", "rename", "delete", "update card", "move sysprompt onto 1st msg", "new_chat_from_card"}
	chatList := make([]string, len(chatMap))
	i := 0
	for name := range chatMap {
		chatList[i] = name
		i++
	}
	rows, cols := len(chatMap), len(actions)+2
	chatActTable := tview.NewTable().
		SetBorders(true)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			switch c {
			case 0:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(chatList[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 1:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(chatMap[chatList[r]].Msgs[len(chatMap[chatList[r]].Msgs)-30:]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			default:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(actions[c-2]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	chatActTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc || key == tcell.KeyF1 {
			pages.RemovePage(historyPage)
			return
		}
		if key == tcell.KeyEnter {
			chatActTable.SetSelectable(true, true)
		}
	}).SetSelectedFunc(func(row int, column int) {
		tc := chatActTable.GetCell(row, column)
		tc.SetTextColor(tcell.ColorRed)
		chatActTable.SetSelectable(false, false)
		selectedChat := chatList[row]
		defer pages.RemovePage(historyPage)
		switch tc.Text {
		case "load":
			history, err := loadHistoryChat(selectedChat)
			if err != nil {
				logger.Error("failed to read history file", "chat", selectedChat)
				pages.RemovePage(historyPage)
				return
			}
			chatBody.Messages = history
			textView.SetText(chatToText(cfg.ShowSys))
			activeChatName = selectedChat
			pages.RemovePage(historyPage)
			return
		case "rename":
			pages.RemovePage(historyPage)
			pages.AddPage(renamePage, renameWindow, true, true)
			return
		case "delete":
			sc, ok := chatMap[selectedChat]
			if !ok {
				// no chat found
				pages.RemovePage(historyPage)
				return
			}
			if err := store.RemoveChat(sc.ID); err != nil {
				logger.Error("failed to remove chat from db", "chat_id", sc.ID, "chat_name", sc.Name)
			}
			if err := notifyUser("chat deleted", selectedChat+" was deleted"); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			// load last chat
			chatBody.Messages = loadOldChatOrGetNew()
			textView.SetText(chatToText(cfg.ShowSys))
			pages.RemovePage(historyPage)
			return
		case "update card":
			// save updated card
			fi := strings.Index(selectedChat, "_")
			agentName := selectedChat[fi+1:]
			cc, ok := sysMap[agentName]
			if !ok {
				logger.Warn("no such card", "agent", agentName)
				//no:lint
				if err := notifyUser("error", "no such card: "+agentName); err != nil {
					logger.Warn("failed ot notify", "error", err)
				}
				return
			}
			// if chatBody.Messages[0].Role != "system" || chatBody.Messages[1].Role != agentName {
			// 	if err := notifyUser("error", "unexpected chat structure; card: "+agentName); err != nil {
			// 		logger.Warn("failed ot notify", "error", err)
			// 	}
			// 	return
			// }
			// change sys_prompt + first msg
			cc.SysPrompt = chatBody.Messages[0].Content
			cc.FirstMsg = chatBody.Messages[1].Content
			if err := pngmeta.WriteToPng(cc.ToSpec(cfg.UserRole), cc.FilePath, cc.FilePath); err != nil {
				logger.Error("failed to write charcard",
					"error", err)
			}
			return
		case "move sysprompt onto 1st msg":
			chatBody.Messages[1].Content = chatBody.Messages[0].Content + chatBody.Messages[1].Content
			chatBody.Messages[0].Content = rpDefenitionSysMsg
			textView.SetText(chatToText(cfg.ShowSys))
			activeChatName = selectedChat
			pages.RemovePage(historyPage)
			return
		case "new_chat_from_card":
			// Reread card from file and start fresh chat
			fi := strings.Index(selectedChat, "_")
			agentName := selectedChat[fi+1:]
			cc, ok := sysMap[agentName]
			if !ok {
				logger.Warn("no such card", "agent", agentName)
				if err := notifyUser("error", "no such card: "+agentName); err != nil {
					logger.Warn("failed to notify", "error", err)
				}
				return
			}
			// Reload card from disk
			newCard, err := pngmeta.ReadCard(cc.FilePath, cfg.UserRole)
			if err != nil {
				logger.Error("failed to reload charcard", "path", cc.FilePath, "error", err)
				newCard, err = pngmeta.ReadCardJson(cc.FilePath)
				if err != nil {
					logger.Error("failed to reload charcard", "path", cc.FilePath, "error", err)
					if err := notifyUser("error", "failed to reload card: "+cc.FilePath); err != nil {
						logger.Warn("failed to notify", "error", err)
					}
					return
				}
			}
			// Update sysMap with fresh card data
			sysMap[agentName] = newCard
			applyCharCard(newCard)
			startNewChat()
			pages.RemovePage(historyPage)
			return
		default:
			return
		}
	})
	return chatActTable
}

// nolint:unused
func makeRAGTable(fileList []string) *tview.Flex {
	actions := []string{"load", "delete"}
	rows, cols := len(fileList), len(actions)+1
	fileTable := tview.NewTable().
		SetBorders(true)
	longStatusView := tview.NewTextView()
	longStatusView.SetText("status text")
	longStatusView.SetBorder(true).SetTitle("status")
	longStatusView.SetChangedFunc(func() {
		app.Draw()
	})
	ragflex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(longStatusView, 0, 10, false).
		AddItem(fileTable, 0, 60, true)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			if c < 1 {
				fileTable.SetCell(r, c,
					tview.NewTableCell(fileList[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			} else {
				fileTable.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	errCh := make(chan error, 1)
	go func() {
		defer pages.RemovePage(RAGPage)
		for {
			select {
			case err := <-errCh:
				if err == nil {
					logger.Error("somehow got a nil err", "error", err)
					continue
				}
				logger.Error("got an err in rag status", "error", err, "textview", longStatusView)
				longStatusView.SetText(fmt.Sprintf("%v", err))
				close(errCh)
				return
			case status := <-rag.LongJobStatusCh:
				longStatusView.SetText(status)
				// fmt.Fprintln(longStatusView, status)
				// app.Sync()
				if status == rag.FinishedRAGStatus {
					close(errCh)
					time.Sleep(2 * time.Second)
					return
				}
			}
		}
	}()
	fileTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc || key == tcell.KeyF1 {
			pages.RemovePage(RAGPage)
			return
		}
		if key == tcell.KeyEnter {
			fileTable.SetSelectable(true, true)
		}
	}).SetSelectedFunc(func(row int, column int) {
		// defer pages.RemovePage(RAGPage)
		tc := fileTable.GetCell(row, column)
		tc.SetTextColor(tcell.ColorRed)
		fileTable.SetSelectable(false, false)
		fpath := fileList[row]
		// notification := fmt.Sprintf("chat: %s; action: %s", fpath, tc.Text)
		switch tc.Text {
		case "load":
			fpath = path.Join(cfg.RAGDir, fpath)
			longStatusView.SetText("clicked load")
			go func() {
				if err := ragger.LoadRAG(fpath); err != nil {
					logger.Error("failed to embed file", "chat", fpath, "error", err)
					errCh <- err
					// pages.RemovePage(RAGPage)
					return
				}
			}()
			return
		case "delete":
			fpath = path.Join(cfg.RAGDir, fpath)
			if err := os.Remove(fpath); err != nil {
				logger.Error("failed to delete file", "filename", fpath, "error", err)
				return
			}
			if err := notifyUser("chat deleted", fpath+" was deleted"); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			return
		default:
			return
		}
	})
	return ragflex
}

// func makeLoadedRAGTable(fileList []string) *tview.Table {
// 	actions := []string{"delete"}
// 	rows, cols := len(fileList), len(actions)+1
// 	fileTable := tview.NewTable().
// 		SetBorders(true)
// 	for r := 0; r < rows; r++ {
// 		for c := 0; c < cols; c++ {
// 			color := tcell.ColorWhite
// 			if c < 1 {
// 				fileTable.SetCell(r, c,
// 					tview.NewTableCell(fileList[r]).
// 						SetTextColor(color).
// 						SetAlign(tview.AlignCenter))
// 			} else {
// 				fileTable.SetCell(r, c,
// 					tview.NewTableCell(actions[c-1]).
// 						SetTextColor(color).
// 						SetAlign(tview.AlignCenter))
// 			}
// 		}
// 	}
// 	fileTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
// 		if key == tcell.KeyEsc || key == tcell.KeyF1 {
// 			pages.RemovePage(RAGPage)
// 			return
// 		}
// 		if key == tcell.KeyEnter {
// 			fileTable.SetSelectable(true, true)
// 		}
// 	}).SetSelectedFunc(func(row int, column int) {
// 		defer pages.RemovePage(RAGPage)
// 		tc := fileTable.GetCell(row, column)
// 		tc.SetTextColor(tcell.ColorRed)
// 		fileTable.SetSelectable(false, false)
// 		fpath := fileList[row]
// 		// notification := fmt.Sprintf("chat: %s; action: %s", fpath, tc.Text)
// 		switch tc.Text {
// 		case "delete":
// 			if err := ragger.RemoveFile(fpath); err != nil {
// 				logger.Error("failed to delete file", "filename", fpath, "error", err)
// 				return
// 			}
// 			if err := notifyUser("chat deleted", fpath+" was deleted"); err != nil {
// 				logger.Error("failed to send notification", "error", err)
// 			}
// 			return
// 		default:
// 			// pages.RemovePage(RAGPage)
// 			return
// 		}
// 	})
// 	return fileTable
// }

func makeAgentTable(agentList []string) *tview.Table {
	actions := []string{"load"}
	rows, cols := len(agentList), len(actions)+1
	chatActTable := tview.NewTable().
		SetBorders(true)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			if c < 1 {
				chatActTable.SetCell(r, c,
					tview.NewTableCell(agentList[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			} else {
				chatActTable.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	chatActTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc || key == tcell.KeyF1 {
			pages.RemovePage(agentPage)
			return
		}
		if key == tcell.KeyEnter {
			chatActTable.SetSelectable(true, true)
		}
	}).SetSelectedFunc(func(row int, column int) {
		tc := chatActTable.GetCell(row, column)
		tc.SetTextColor(tcell.ColorRed)
		chatActTable.SetSelectable(false, false)
		selected := agentList[row]
		// notification := fmt.Sprintf("chat: %s; action: %s", selectedChat, tc.Text)
		switch tc.Text {
		case "load":
			if ok := charToStart(selected); !ok {
				logger.Warn("no such sys msg", "name", selected)
				pages.RemovePage(agentPage)
				return
			}
			// replace textview
			textView.SetText(chatToText(cfg.ShowSys))
			colorText()
			updateStatusLine()
			// sysModal.ClearButtons()
			pages.RemovePage(agentPage)
			app.SetFocus(textArea)
			return
		case "rename":
			pages.RemovePage(agentPage)
			pages.AddPage(renamePage, renameWindow, true, true)
			return
		case "delete":
			sc, ok := chatMap[selected]
			if !ok {
				// no chat found
				pages.RemovePage(agentPage)
				return
			}
			if err := store.RemoveChat(sc.ID); err != nil {
				logger.Error("failed to remove chat from db", "chat_id", sc.ID, "chat_name", sc.Name)
			}
			if err := notifyUser("chat deleted", selected+" was deleted"); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			pages.RemovePage(agentPage)
			return
		default:
			pages.RemovePage(agentPage)
			return
		}
	})
	return chatActTable
}

func makeCodeBlockTable(codeBlocks []string) *tview.Table {
	actions := []string{"copy"}
	rows, cols := len(codeBlocks), len(actions)+1
	table := tview.NewTable().
		SetBorders(true)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			previewLen := 30
			if len(codeBlocks[r]) < 30 {
				previewLen = len(codeBlocks[r])
			}
			if c < 1 {
				table.SetCell(r, c,
					tview.NewTableCell(codeBlocks[r][:previewLen]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			} else {
				table.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	table.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc || key == tcell.KeyF1 {
			pages.RemovePage(agentPage)
			return
		}
		if key == tcell.KeyEnter {
			table.SetSelectable(true, true)
		}
	}).SetSelectedFunc(func(row int, column int) {
		tc := table.GetCell(row, column)
		tc.SetTextColor(tcell.ColorRed)
		table.SetSelectable(false, false)
		selected := codeBlocks[row]
		// notification := fmt.Sprintf("chat: %s; action: %s", selectedChat, tc.Text)
		switch tc.Text {
		case "copy":
			if err := copyToClipboard(selected); err != nil {
				if err := notifyUser("error", err.Error()); err != nil {
					logger.Error("failed to send notification", "error", err)
				}
			}
			if err := notifyUser("copied", selected); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			pages.RemovePage(codeBlockPage)
			app.SetFocus(textArea)
			return
		default:
			pages.RemovePage(codeBlockPage)
			return
		}
	})
	return table
}

func makeImportChatTable(filenames []string) *tview.Table {
	actions := []string{"load"}
	rows, cols := len(filenames), len(actions)+1
	chatActTable := tview.NewTable().
		SetBorders(true)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			if c < 1 {
				chatActTable.SetCell(r, c,
					tview.NewTableCell(filenames[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			} else {
				chatActTable.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	chatActTable.Select(0, 0).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc || key == tcell.KeyF1 {
			pages.RemovePage(historyPage)
			return
		}
		if key == tcell.KeyEnter {
			chatActTable.SetSelectable(true, true)
		}
	}).SetSelectedFunc(func(row int, column int) {
		tc := chatActTable.GetCell(row, column)
		tc.SetTextColor(tcell.ColorRed)
		chatActTable.SetSelectable(false, false)
		selected := filenames[row]
		// notification := fmt.Sprintf("chat: %s; action: %s", selectedChat, tc.Text)
		switch tc.Text {
		case "load":
			if err := importChat(selected); err != nil {
				logger.Warn("failed to import chat", "filename", selected)
				pages.RemovePage(historyPage)
				return
			}
			colorText()
			updateStatusLine()
			// redraw the text in text area
			textView.SetText(chatToText(cfg.ShowSys))
			pages.RemovePage(historyPage)
			app.SetFocus(textArea)
			return
		case "rename":
			pages.RemovePage(historyPage)
			pages.AddPage(renamePage, renameWindow, true, true)
			return
		case "delete":
			sc, ok := chatMap[selected]
			if !ok {
				// no chat found
				pages.RemovePage(historyPage)
				return
			}
			if err := store.RemoveChat(sc.ID); err != nil {
				logger.Error("failed to remove chat from db", "chat_id", sc.ID, "chat_name", sc.Name)
			}
			if err := notifyUser("chat deleted", selected+" was deleted"); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			pages.RemovePage(historyPage)
			return
		default:
			pages.RemovePage(historyPage)
			return
		}
	})
	return chatActTable
}

func makeFilePicker() *tview.Flex {
	// Initialize with directory from config or current directory
	currentDir := cfg.FilePickerDir
	if currentDir == "" {
		currentDir = "."
	}

	// Track navigation history
	dirStack := []string{currentDir}
	currentStackPos := 0

	// Track selected file
	var selectedFile string

	// Create UI elements
	listView := tview.NewList()
	listView.SetBorder(true).SetTitle("Files & Directories").SetTitleAlign(tview.AlignLeft)

	statusView := tview.NewTextView()
	statusView.SetBorder(true).SetTitle("Selected File").SetTitleAlign(tview.AlignLeft)
	statusView.SetTextColor(tcell.ColorYellow)

	buttonBar := tview.NewFlex()

	// Button functions
	loadButton := tview.NewButton("Load")
	loadButton.SetSelectedFunc(func() {
		if selectedFile != "" {
			// Update the global text area with the selected file path
			textArea.SetText(selectedFile, true)
			app.SetFocus(textArea)
		}
		pages.RemovePage(filePickerPage)
	})

	cancelButton := tview.NewButton("Cancel")
	cancelButton.SetSelectedFunc(func() {
		pages.RemovePage(filePickerPage)
	})

	buttonBar.AddItem(tview.NewBox().SetBackgroundColor(tcell.ColorDefault), 0, 1, false)
	buttonBar.AddItem(loadButton, 8, 1, true)
	buttonBar.AddItem(tview.NewBox(), 1, 1, false)
	buttonBar.AddItem(cancelButton, 8, 1, true)
	buttonBar.AddItem(tview.NewBox().SetBackgroundColor(tcell.ColorDefault), 0, 1, false)

	// Layout
	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(listView, 0, 3, true)
	flex.AddItem(statusView, 3, 0, false)
	flex.AddItem(buttonBar, 3, 0, false)

	// Refresh the file list
	var refreshList func(string)
	refreshList = func(dir string) {
		listView.Clear()

		// Add parent directory (..) if not at root
		if dir != "/" {
			parentDir := path.Dir(dir)
			// Special handling for edge cases - only return if we're truly at a system root
			// For Unix-like systems, path.Dir("/") returns "/" which would cause parentDir == dir
			if parentDir == dir && dir == "/" {
				// We're at the root ("/") and trying to go up, just don't add the parent item
			} else {
				listView.AddItem("../", "(Parent Directory)", 'p', func() {
					refreshList(parentDir)
					dirStack = append(dirStack, parentDir)
					currentStackPos = len(dirStack) - 1
				})
			}
		}

		// Read directory contents
		files, err := os.ReadDir(dir)
		if err != nil {
			statusView.SetText("Error reading directory: " + err.Error())
			return
		}

		// Add directories and files to the list
		for _, file := range files {
			name := file.Name()
			if file.IsDir() {
				listView.AddItem(name+"/", "(Directory)", 0, func() {
					newDir := path.Join(dir, name)
					refreshList(newDir)
					dirStack = append(dirStack, newDir)
					currentStackPos = len(dirStack) - 1
					statusView.SetText("Current: " + newDir)
				})
			} else {
				listView.AddItem(name, "(File)", 0, func() {
					selectedFile = path.Join(dir, name)
					statusView.SetText("Selected: " + selectedFile)
				})
			}
		}

		statusView.SetText("Current: " + dir)
	}

	// Initialize the file list
	refreshList(currentDir)

	// Set up keyboard navigation
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			pages.RemovePage(filePickerPage)
			return nil
		case tcell.KeyBackspace2: // Backspace to go to parent directory
			if currentStackPos > 0 {
				currentStackPos--
				prevDir := dirStack[currentStackPos]
				refreshList(prevDir)
				// Trim the stack to current position to avoid deep history
				dirStack = dirStack[:currentStackPos+1]
			}
			return nil
		case tcell.KeyEnter:
			// Get the currently highlighted item in the list
			itemIndex := listView.GetCurrentItem()
			if itemIndex >= 0 && itemIndex < listView.GetItemCount() {
				// We need to get the text of the currently selected item to determine if it's a directory
				// Since we can't directly get the item text, we'll keep track of items differently
				// Let's improve the approach by tracking the currently selected item
				itemText, _ := listView.GetItemText(itemIndex)

				// Check if it's a directory (typically ends with /)
				if strings.HasSuffix(itemText, "/") {
					// This is a directory, we need to get the full path
					// Since the item text ends with "/" and represents a directory
					var targetDir string
					if strings.HasPrefix(itemText, "../") {
						// Parent directory - need to go up from current directory
						targetDir = path.Dir(currentDir)
						// Avoid going above root - if parent is same as current and it's system root
						if targetDir == currentDir && currentDir == "/" {
							// We're at root, don't navigate
							return nil
						}
					} else {
						// Regular subdirectory
						dirName := strings.TrimSuffix(itemText, "/")
						targetDir = path.Join(currentDir, dirName)
					}

					// Navigate to the selected directory
					refreshList(targetDir)
					dirStack = append(dirStack, targetDir)
					currentStackPos = len(dirStack) - 1
					statusView.SetText("Current: " + targetDir)
					return nil
				} else {
					// It's a file, load it if one was selected
					if selectedFile != "" {
						textArea.SetText(selectedFile, true)
						app.SetFocus(textArea)
						pages.RemovePage(filePickerPage)
					}
					return nil
				}
			}
			return nil
		}
		return event
	})

	return flex
}
