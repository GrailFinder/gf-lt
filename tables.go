package main

import (
	"fmt"
	"image"
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
	// Sort chatList by UpdatedAt field in descending order (most recent first)
	for i := 0; i < len(chatList)-1; i++ {
		for j := i + 1; j < len(chatList); j++ {
			if chatMap[chatList[i]].UpdatedAt.Before(chatMap[chatList[j]].UpdatedAt) {
				// Swap chatList[i] and chatList[j]
				chatList[i], chatList[j] = chatList[j], chatList[i]
			}
		}
	}
	// Add 1 extra row for header
	rows, cols := len(chatMap)+1, len(actions)+4 // +2 for name, +2 for timestamps
	chatActTable := tview.NewTable().
		SetBorders(true)
	// Add header row (row 0)
	for c := 0; c < cols; c++ {
		color := tcell.ColorWhite
		var headerText string
		switch c {
		case 0:
			headerText = "Chat Name"
		case 1:
			headerText = "Preview"
		case 2:
			headerText = "Created At"
		case 3:
			headerText = "Updated At"
		default:
			headerText = actions[c-4]
		}
		chatActTable.SetCell(0, c,
			tview.NewTableCell(headerText).
				SetSelectable(false).
				SetTextColor(color).
				SetAlign(tview.AlignCenter).
				SetAttributes(tcell.AttrBold))
	}
	previewLen := 100
	// Add data rows (starting from row 1)
	for r := 0; r < rows-1; r++ { // rows-1 because we added a header row
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			switch c {
			case 0:
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(chatList[r]).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 1:
				if len(chatMap[chatList[r]].Msgs) < 100 {
					previewLen = len(chatMap[chatList[r]].Msgs)
				}
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(chatMap[chatList[r]].Msgs[len(chatMap[chatList[r]].Msgs)-previewLen:]).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 2:
				// Created At column
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(chatMap[chatList[r]].CreatedAt.Format("2006-01-02 15:04")).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 3:
				// Updated At column
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(chatMap[chatList[r]].UpdatedAt.Format("2006-01-02 15:04")).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			default:
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(actions[c-4]). // Adjusted offset to account for 2 new timestamp columns
										SetTextColor(color).
										SetAlign(tview.AlignCenter))
			}
		}
	}
	chatActTable.Select(1, 0).SetSelectable(true, true).SetFixed(1, 1).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc || key == tcell.KeyF1 || key == tcell.Key('x') {
			pages.RemovePage(historyPage)
			return
		}
	}).SetSelectedFunc(func(row int, column int) {
		// Skip header row (row 0) for selection
		if row == 0 {
			// If user clicks on header, just return without action
			chatActTable.Select(1, column) // Move selection to first data row
			return
		}
		tc := chatActTable.GetCell(row, column)
		tc.SetTextColor(tcell.ColorRed)
		chatActTable.SetSelectable(false, false)
		selectedChat := chatList[row-1] // -1 to account for header row
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
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
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
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
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
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
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
			// fetching sysprompt and first message anew from the card
			startNewChat(false)
			pages.RemovePage(historyPage)
			return
		default:
			return
		}
	})
	// Add input capture to handle 'x' key for closing the table
	chatActTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(historyPage)
			return nil
		}
		return event
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
	// Add the exit option as the first row (row 0)
	fileTable.SetCell(0, 0,
		tview.NewTableCell("Exit RAG manager").
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	fileTable.SetCell(0, 1,
		tview.NewTableCell("(Close without action)").
			SetTextColor(tcell.ColorGray).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	fileTable.SetCell(0, 2,
		tview.NewTableCell("exit").
			SetTextColor(tcell.ColorGray).
			SetAlign(tview.AlignCenter))
	// Add the file rows starting from row 1
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			switch {
			case c < 1:
				fileTable.SetCell(r+1, c, // +1 to account for the exit row at index 0
					tview.NewTableCell(fileList[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			case c == 1: // Action description column - not selectable
				fileTable.SetCell(r+1, c, // +1 to account for the exit row at index 0
					tview.NewTableCell("(Action)").
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			default: // Action button column - selectable
				fileTable.SetCell(r+1, c, // +1 to account for the exit row at index 0
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	errCh := make(chan error, 1) // why?
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
	fileTable.Select(0, 0).
		SetFixed(1, 1).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorWhite)).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEsc || key == tcell.KeyF1 || key == tcell.Key('x') || key == tcell.KeyCtrlX {
				pages.RemovePage(RAGPage)
				return
			}
		}).SetSelectedFunc(func(row int, column int) {
		// If user selects a non-actionable column (0 or 1), move to first action column (2)
		if column <= 1 {
			if fileTable.GetColumnCount() > 2 {
				fileTable.Select(row, 2) // Select first action column
			}
			return
		}
		// defer pages.RemovePage(RAGPage)
		tc := fileTable.GetCell(row, column)
		// Check if the selected row is the exit row (row 0) - do this first to avoid index issues
		if row == 0 {
			pages.RemovePage(RAGPage)
			return
		}
		// For file rows, get the filename (row index - 1 because of the exit row at index 0)
		fpath := fileList[row-1] // -1 to account for the exit row at index 0
		// notification := fmt.Sprintf("chat: %s; action: %s", fpath, tc.Text)
		switch tc.Text {
		case "load":
			fpath = path.Join(cfg.RAGDir, fpath)
			longStatusView.SetText("clicked load")
			go func() {
				if err := ragger.LoadRAG(fpath); err != nil {
					logger.Error("failed to embed file", "chat", fpath, "error", err)
					_ = notifyUser("RAG", "failed to embed file; error: "+err.Error())
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
			pages.RemovePage(RAGPage)
			return
		}
	})
	// Add input capture to the flex container to handle 'x' key for closing
	ragflex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(RAGPage)
			return nil
		}
		return event
	})
	return ragflex
}

func makeLoadedRAGTable(fileList []string) *tview.Flex {
	actions := []string{"delete"}
	rows, cols := len(fileList), len(actions)+1
	// Add 1 extra row for the "exit" option at the top
	fileTable := tview.NewTable().
		SetBorders(true)
	longStatusView := tview.NewTextView()
	longStatusView.SetText("Loaded RAG files list")
	longStatusView.SetBorder(true).SetTitle("status")
	longStatusView.SetChangedFunc(func() {
		app.Draw()
	})
	ragflex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(longStatusView, 0, 10, false).
		AddItem(fileTable, 0, 60, true)
	// Add the exit option as the first row (row 0)
	fileTable.SetCell(0, 0,
		tview.NewTableCell("Exit Loaded Files manager").
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	fileTable.SetCell(0, 1,
		tview.NewTableCell("(Close without action)").
			SetTextColor(tcell.ColorGray).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	fileTable.SetCell(0, 2,
		tview.NewTableCell("exit").
			SetTextColor(tcell.ColorGray).
			SetAlign(tview.AlignCenter))
	// Add the file rows starting from row 1
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			switch {
			case c < 1:
				fileTable.SetCell(r+1, c, // +1 to account for the exit row at index 0
					tview.NewTableCell(fileList[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			case c == 1: // Action description column - not selectable
				fileTable.SetCell(r+1, c, // +1 to account for the exit row at index 0
					tview.NewTableCell("(Action)").
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			default: // Action button column - selectable
				fileTable.SetCell(r+1, c, // +1 to account for the exit row at index 0
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	fileTable.Select(0, 0).
		SetFixed(1, 1).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorWhite)).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEsc || key == tcell.KeyF1 || key == tcell.Key('x') || key == tcell.KeyCtrlX {
				pages.RemovePage(RAGLoadedPage)
				return
			}
		}).SetSelectedFunc(func(row int, column int) {
		// If user selects a non-actionable column (0 or 1), move to first action column (2)
		if column <= 1 {
			if fileTable.GetColumnCount() > 2 {
				fileTable.Select(row, 2) // Select first action column
			}
			return
		}
		tc := fileTable.GetCell(row, column)
		// Check if the selected row is the exit row (row 0) - do this first to avoid index issues
		if row == 0 {
			pages.RemovePage(RAGLoadedPage)
			return
		}
		// For file rows, get the filename (row index - 1 because of the exit row at index 0)
		fpath := fileList[row-1] // -1 to account for the exit row at index 0
		switch tc.Text {
		case "delete":
			if err := ragger.RemoveFile(fpath); err != nil {
				logger.Error("failed to delete file from RAG", "filename", fpath, "error", err)
				longStatusView.SetText(fmt.Sprintf("Error deleting file: %v", err))
				return
			}
			if err := notifyUser("RAG file deleted", fpath+" was deleted from RAG system"); err != nil {
				logger.Error("failed to send notification", "error", err)
			}
			longStatusView.SetText(fpath + " was deleted from RAG system")
			return
		default:
			pages.RemovePage(RAGLoadedPage)
			return
		}
	})
	// Add input capture to the flex container to handle 'x' key for closing
	ragflex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(RAGLoadedPage)
			return nil
		}
		return event
	})
	return ragflex
}

func makeAgentTable(agentList []string) *tview.Table {
	actions := []string{"filepath", "load"}
	rows, cols := len(agentList), len(actions)+1
	chatActTable := tview.NewTable().
		SetBorders(true)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			switch {
			case c < 1:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(agentList[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			case c == 1:
				if actions[c-1] == "filepath" {
					cc, ok := sysMap[agentList[r]]
					if !ok {
						continue
					}
					chatActTable.SetCell(r, c,
						tview.NewTableCell(cc.FilePath).
							SetTextColor(color).
							SetAlign(tview.AlignCenter).
							SetSelectable(false))
					continue
				}
				chatActTable.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			default:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	chatActTable.Select(0, 0).
		SetFixed(1, 1).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorWhite)).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEsc || key == tcell.KeyF1 || key == tcell.Key('x') {
				pages.RemovePage(agentPage)
				return
			}
		}).SetSelectedFunc(func(row int, column int) {
		// If user selects a non-actionable column (0 or 1), move to first action column (2)
		if column <= 1 {
			if chatActTable.GetColumnCount() > 2 {
				chatActTable.Select(row, 2) // Select first action column
			}
			return
		}
		tc := chatActTable.GetCell(row, column)
		selected := agentList[row]
		// notification := fmt.Sprintf("chat: %s; action: %s", selectedChat, tc.Text)
		switch tc.Text {
		case "load":
			if ok := charToStart(selected, true); !ok {
				logger.Warn("no such sys msg", "name", selected)
				pages.RemovePage(agentPage)
				return
			}
			// replace textview
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
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
	// Add input capture to handle 'x' key for closing the table
	chatActTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(agentPage)
			return nil
		}
		return event
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
			switch {
			case c < 1:
				table.SetCell(r, c,
					tview.NewTableCell(codeBlocks[r][:previewLen]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			default:
				table.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	table.Select(0, 0).
		SetFixed(1, 1).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorWhite)).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEsc || key == tcell.KeyF1 || key == tcell.Key('x') {
				pages.RemovePage(codeBlockPage)
				return
			}
		}).SetSelectedFunc(func(row int, column int) {
		// If user selects a non-actionable column (0), move to first action column (1)
		if column == 0 {
			if table.GetColumnCount() > 1 {
				table.Select(row, 1) // Select first action column
			}
			return
		}
		tc := table.GetCell(row, column)
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
	// Add input capture to handle 'x' key for closing the table
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(codeBlockPage)
			return nil
		}
		return event
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
			switch {
			case c < 1:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(filenames[r]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			default:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(actions[c-1]).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
		}
	}
	chatActTable.Select(0, 0).
		SetFixed(1, 1).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorWhite)).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEsc || key == tcell.KeyF1 || key == tcell.Key('x') {
				pages.RemovePage(historyPage)
				return
			}
		}).SetSelectedFunc(func(row int, column int) {
		// If user selects a non-actionable column (0), move to first action column (1)
		if column == 0 {
			if chatActTable.GetColumnCount() > 1 {
				chatActTable.Select(row, 1) // Select first action column
			}
			return
		}
		tc := chatActTable.GetCell(row, column)
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
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
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
	// Add input capture to handle 'x' key for closing the table
	chatActTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(historyPage)
			return nil
		}
		return event
	})
	return chatActTable
}

func makeFilePicker() *tview.Flex {
	// Initialize with directory from config or current directory
	startDir := cfg.FilePickerDir
	if startDir == "" {
		startDir = "."
	}
	// If startDir is ".", resolve it to the actual current working directory
	if startDir == "." {
		wd, err := os.Getwd()
		if err == nil {
			startDir = wd
		}
	}
	// Track navigation history
	dirStack := []string{startDir}
	currentStackPos := 0
	// Track selected file
	var selectedFile string
	// Track currently displayed directory (changes as user navigates)
	currentDisplayDir := startDir
	// Helper function to check if a file has an allowed extension from config
	hasAllowedExtension := func(filename string) bool {
		// If no allowed extensions are specified in config, allow all files
		if cfg.FilePickerExts == "" {
			return true
		}
		// Split the allowed extensions from the config string
		allowedExts := strings.Split(cfg.FilePickerExts, ",")
		lowerFilename := strings.ToLower(strings.TrimSpace(filename))
		for _, ext := range allowedExts {
			ext = strings.TrimSpace(ext) // Remove any whitespace around the extension
			if ext != "" && strings.HasSuffix(lowerFilename, "."+ext) {
				return true
			}
		}
		return false
	}
	// Helper function to check if a file is an image
	isImageFile := func(filename string) bool {
		imageExtensions := []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff", ".svg"}
		lowerFilename := strings.ToLower(filename)
		for _, ext := range imageExtensions {
			if strings.HasSuffix(lowerFilename, ext) {
				return true
			}
		}
		return false
	}
	// Create UI elements
	listView := tview.NewList()
	listView.SetBorder(true).SetTitle("Files & Directories").SetTitleAlign(tview.AlignLeft)
	// Status view for selected file information
	statusView := tview.NewTextView()
	statusView.SetBorder(true).SetTitle("Selected File").SetTitleAlign(tview.AlignLeft)
	statusView.SetTextColor(tcell.ColorYellow)
	// Image preview pane
	var imgPreview *tview.Image
	if cfg.ImagePreview {
		imgPreview = tview.NewImage()
		imgPreview.SetBorder(true).SetTitle("Preview").SetTitleAlign(tview.AlignLeft)
	}
	// Horizontal flex for list + preview
	var hFlex *tview.Flex
	if cfg.ImagePreview && imgPreview != nil {
		hFlex = tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(listView, 0, 3, true).
			AddItem(imgPreview, 0, 2, false)
	} else {
		hFlex = tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(listView, 0, 1, true)
	}
	// Main vertical flex
	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(hFlex, 0, 3, true)
	flex.AddItem(statusView, 3, 0, false)
	// Refresh the file list
	var refreshList func(string)
	refreshList = func(dir string) {
		listView.Clear()
		// Update the current display directory
		currentDisplayDir = dir // Update the current display directory
		// Add exit option at the top
		listView.AddItem("Exit file picker [gray](Close without selecting)[-]", "", 'x', func() {
			pages.RemovePage(filePickerPage)
		})
		// Add parent directory (..) if not at root
		if dir != "/" {
			parentDir := path.Dir(dir)
			// Special handling for edge cases - only return if we're truly at a system root
			// For Unix-like systems, path.Dir("/") returns "/" which would cause parentDir == dir
			if parentDir == dir && dir == "/" {
				// We're at the root ("/") and trying to go up, just don't add the parent item
			} else {
				listView.AddItem("../ [gray](Parent Directory)[-]", "", 'p', func() {
					imgPreview.SetImage(nil)
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
			// Skip hidden files and directories (those starting with a dot)
			if strings.HasPrefix(name, ".") {
				continue
			}
			if file.IsDir() {
				// Capture the directory name for the closure to avoid loop variable issues
				dirName := name
				listView.AddItem(dirName+"/ [gray](Directory)[-]", "", 0, func() {
					imgPreview.SetImage(nil)
					newDir := path.Join(dir, dirName)
					refreshList(newDir)
					dirStack = append(dirStack, newDir)
					currentStackPos = len(dirStack) - 1
					statusView.SetText("Current: " + newDir)
				})
			} else if hasAllowedExtension(name) {
				// Only show files that have allowed extensions (from config)
				// Capture the file name for the closure to avoid loop variable issues
				fileName := name
				fullFilePath := path.Join(dir, fileName)
				listView.AddItem(fileName+" [gray](File)[-]", "", 0, func() {
					selectedFile = fullFilePath
					statusView.SetText("Selected: " + selectedFile)
					// Check if the file is an image
					if isImageFile(fileName) {
						// For image files, offer to attach to the next LLM message
						statusView.SetText("Selected image: " + selectedFile)
					} else {
						// For non-image files, display as before
						statusView.SetText("Selected: " + selectedFile)
					}
				})
			}
		}
		statusView.SetText("Current: " + dir)
	}
	// Initialize the file list
	refreshList(startDir)
	// Update image preview when selection changes
	if cfg.ImagePreview && imgPreview != nil {
		listView.SetChangedFunc(func(index int, mainText, secondaryText string, rune rune) {
			itemText, _ := listView.GetItemText(index)
			if strings.HasPrefix(itemText, "Exit file picker") || strings.HasPrefix(itemText, "../") {
				imgPreview.SetImage(nil)
				return
			}
			actualItemName := itemText
			if bracketPos := strings.Index(itemText, " ["); bracketPos != -1 {
				actualItemName = itemText[:bracketPos]
			}
			if strings.HasSuffix(actualItemName, "/") {
				imgPreview.SetImage(nil)
				return
			}
			if !isImageFile(actualItemName) {
				imgPreview.SetImage(nil)
				return
			}
			filePath := path.Join(currentDisplayDir, actualItemName)
			file, err := os.Open(filePath)
			if err != nil {
				imgPreview.SetImage(nil)
				return
			}
			defer file.Close()
			img, _, err := image.Decode(file)
			if err != nil {
				imgPreview.SetImage(nil)
				return
			}
			imgPreview.SetImage(img)
		})
	}
	// Set up keyboard navigation
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			pages.RemovePage(filePickerPage)
			return nil
		case tcell.KeyBackspace2: // Backspace to go to parent directory
			if cfg.ImagePreview && imgPreview != nil {
				imgPreview.SetImage(nil)
			}
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
				logger.Info("choosing dir", "itemText", itemText)
				// Check for the exit option first (should be the first item)
				if strings.HasPrefix(itemText, "Exit file picker") {
					pages.RemovePage(filePickerPage)
					return nil
				}
				// Extract the actual filename/directory name by removing the type info in brackets
				// Format is "name [gray](type)[-]"
				actualItemName := itemText
				if bracketPos := strings.Index(itemText, " ["); bracketPos != -1 {
					actualItemName = itemText[:bracketPos]
				}
				// Check if it's a directory (ends with /)
				if strings.HasSuffix(actualItemName, "/") {
					// This is a directory, we need to get the full path
					// Since the item text ends with "/" and represents a directory
					var targetDir string
					if strings.HasPrefix(actualItemName, "../") {
						// Parent directory - need to go up from current directory
						targetDir = path.Dir(currentDisplayDir)
						// Avoid going above root - if parent is same as current and it's system root
						if targetDir == currentDisplayDir && currentDisplayDir == "/" {
							// We're at root, don't navigate
							logger.Warn("went to root", "dir", targetDir)
							return nil
						}
					} else {
						// Regular subdirectory
						dirName := strings.TrimSuffix(actualItemName, "/")
						targetDir = path.Join(currentDisplayDir, dirName)
					}
					// Navigate to the selected directory
					logger.Info("going to the dir", "dir", targetDir)
					if cfg.ImagePreview && imgPreview != nil {
						imgPreview.SetImage(nil)
					}
					refreshList(targetDir)
					dirStack = append(dirStack, targetDir)
					currentStackPos = len(dirStack) - 1
					statusView.SetText("Current: " + targetDir)
					return nil
				} else {
					// It's a file - construct the full path from current directory and the actual item name
					// We can't rely only on the selectedFile variable since Enter key might be pressed
					// without having clicked the file first
					filePath := path.Join(currentDisplayDir, actualItemName)
					// Verify it's actually a file (not just lacking a directory suffix)
					if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
						// Check if the file is an image
						if isImageFile(actualItemName) {
							// For image files, set it as an attachment for the next LLM message
							// Use the version without UI updates to avoid hangs in event handlers
							logger.Info("setting image", "file", actualItemName)
							SetImageAttachment(filePath)
							logger.Info("after setting image", "file", actualItemName)
							statusView.SetText("Image attached: " + filePath + " (will be sent with next message)")
							logger.Info("after setting text", "file", actualItemName)
							pages.RemovePage(filePickerPage)
							logger.Info("after update drawn", "file", actualItemName)
						} else {
							// For non-image files, update the text area with file path
							textArea.SetText(filePath, true)
							app.SetFocus(textArea)
							pages.RemovePage(filePickerPage)
						}
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
