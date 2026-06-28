package main

import (
	"encoding/json"
	"fmt"
	"gf-lt/tools"
	"strconv"
	"image"
	"os"
	"path"
	"strings"
	"time"

	"gf-lt/models"
	"gf-lt/pngmeta"
	"gf-lt/rag"

	"github.com/gdamore/tcell/v2"
	"github.com/nfnt/resize"
	"github.com/rivo/tview"
	"gitlab.com/diamondburned/ueberzug-go"
)

var currentFilePickerUeberzugImg *ueberzug.Image

func getChatSummary(msgsJSON string, maxMsgs int) string {
	var msgs []models.RoleMsg
	if err := json.Unmarshal([]byte(msgsJSON), &msgs); err != nil {
		return "error"
	}
	var userMsgs []string
	for i := range msgs {
		m := &msgs[i]
		if m.Role == "user" {
			content := m.Content
			if len(content) > 40 {
				content = content[:40] + "..."
			}
			userMsgs = append(userMsgs, content)
			if len(userMsgs) >= maxMsgs {
				break
			}
		}
	}
	if len(userMsgs) == 0 {
		return "(no user msgs)"
	}
	return strings.Join(userMsgs, " | ")
}

func getMsgCount(msgsJSON string) string {
	var msgs []models.RoleMsg
	if err := json.Unmarshal([]byte(msgsJSON), &msgs); err != nil {
		return "0"
	}
	return strconv.Itoa(len(msgs))
}

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
	rows, cols := len(chatMap)+1, len(actions)+5 // +2 for name, +2 for timestamps, +1 for msg count
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
			headerText = "Msg Count"
		case 3:
			headerText = "Created At"
		case 4:
			headerText = "Updated At"
		default:
			headerText = actions[c-5]
		}
		chatActTable.SetCell(0, c,
			tview.NewTableCell(headerText).
				SetSelectable(false).
				SetTextColor(color).
				SetAlign(tview.AlignCenter).
				SetAttributes(tcell.AttrBold))
	}
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
				chatActTable.SetCell(r+1, c,
					tview.NewTableCell(getChatSummary(chatMap[chatList[r]].Msgs, 3)).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 2:
				chatActTable.SetCell(r+1, c,
					tview.NewTableCell(getMsgCount(chatMap[chatList[r]].Msgs)).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 3:
				// Created At column
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(chatMap[chatList[r]].CreatedAt.Format("2006-01-02 15:04")).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 4:
				// Updated At column
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(chatMap[chatList[r]].UpdatedAt.Format("2006-01-02 15:04")).
						SetSelectable(false).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			default:
				chatActTable.SetCell(r+1, c, // +1 to account for header row
					tview.NewTableCell(actions[c-5]). // Adjusted offset to account for new columns
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
			colorText()
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
			showToast("chat deleted", selectedChat+" was deleted")
			// load last chat
			chatBody.Messages = loadOldChatOrGetNew()
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			pages.RemovePage(historyPage)
			return
		case "update card":
			// save updated card
			fi := strings.Index(selectedChat, "_")
			agentName := selectedChat[fi+1:]
			cc := GetCardByRole(agentName)
			if cc == nil {
				logger.Warn("no such card", "agent", agentName)
				showToast("error", "no such card: "+agentName)
				return
			}
			// strip <tool_guide> before saving so it doesn't get embedded in the card file
			cleaned := removeToolGuide([]models.RoleMsg{{Role: "system", Content: chatBody.Messages[0].Content}})
			cc.SysPrompt = cleaned[0].Content
			cc.FirstMsg = chatBody.Messages[1].Content
			if err := pngmeta.WriteToPng(cc.ToSpec(cfg.UserRole), cc.FilePath, cc.FilePath); err != nil {
				logger.Error("failed to write charcard", "error", err)
			}
			return
		case "move sysprompt onto 1st msg":
			chatBody.Messages[1].Content = chatBody.Messages[0].Content + chatBody.Messages[1].Content
			chatBody.Messages[0].Content = tools.RpDefenitionSysMsg
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			activeChatName = selectedChat
			pages.RemovePage(historyPage)
			return
		case "new_chat_from_card":
			ch, ok := chatMap[selectedChat]
			if !ok {
				showToast("error", "chat not found")
				return
			}
			cc, ok := sysMap[ch.Agent]
			if !ok {
				logger.Warn("no such card", "agent", ch.Agent)
				showToast("error", "no such card: "+ch.Agent)
				return
			}
			newCard, err := pngmeta.ReadCard(cc.FilePath, cfg.UserRole)
			if err != nil {
				logger.Error("failed to reload charcard", "path", cc.FilePath, "error", err)
				newCard, err = pngmeta.ReadCardJson(cc.FilePath, cfg.UserRole)
				if err != nil {
					logger.Error("failed to reload charcard", "path", cc.FilePath, "error", err)
					showToast("error", "failed to reload card: "+cc.FilePath)
					return
				}
			}
			if newCard.ID == "" {
				newCard.ID = models.ComputeCardID(newCard.Role, newCard.FilePath)
			}
			sysMap[newCard.ID] = newCard
			roleToID[newCard.Role] = newCard.ID
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
func formatSize(size int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	s := float64(size)
	for s >= 1024 && i < len(units)-1 {
		s /= 1024
		i++
	}
	return fmt.Sprintf("%.1f%s", s, units[i])
}

type ragFileInfo struct {
	name     string
	inRAGDir bool
	isLoaded bool
	fullPath string
}

func makeRAGTable(fileList []string, loadedFiles []string) *tview.Flex {
	// Build set of loaded files for quick lookup
	loadedSet := make(map[string]bool)
	for _, f := range loadedFiles {
		loadedSet[f] = true
	}
	// Build merged list: files from ragdir + orphaned files from DB
	ragFiles := make([]ragFileInfo, 0, len(fileList)+len(loadedFiles))
	seen := make(map[string]bool)
	// Add files from ragdir
	for _, f := range fileList {
		ragFiles = append(ragFiles, ragFileInfo{
			name:     f,
			inRAGDir: true,
			isLoaded: loadedSet[f],
			fullPath: path.Join(cfg.RAGDir, f),
		})
		seen[f] = true
	}
	// Add orphaned files (in DB but not in ragdir)
	for _, f := range loadedFiles {
		if !seen[f] {
			ragFiles = append(ragFiles, ragFileInfo{
				name:     f,
				inRAGDir: false,
				isLoaded: true,
				fullPath: "",
			})
		}
	}
	rows := len(ragFiles)
	cols := 4 // File Name | Preview | Action | Delete
	fileTable := tview.NewTable().
		SetBorders(true)
	longStatusView := tview.NewTextView()
	longStatusView.SetText("press x to exit | press d to view DB")
	longStatusView.SetBorder(true).SetTitle("status")
	longStatusView.SetChangedFunc(func() {
		app.Draw()
	})
	ragflex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(longStatusView, 0, 10, false).
		AddItem(fileTable, 0, 60, true)
	// Add the exit option as the first row (row 0)
	fileTable.SetCell(0, 0,
		tview.NewTableCell("File Name").
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	fileTable.SetCell(0, 1,
		tview.NewTableCell("Preview").
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	fileTable.SetCell(0, 2,
		tview.NewTableCell("Load/Unload").
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	fileTable.SetCell(0, 3,
		tview.NewTableCell("Delete").
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	// Add the file rows starting from row 1
	for r := 0; r < rows; r++ {
		f := ragFiles[r]
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			switch c {
			case 0:
				displayName := f.name
				if !f.inRAGDir {
					displayName = f.name + " (orphaned)"
				}
				fileTable.SetCell(r+1, c,
					tview.NewTableCell(displayName).
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			case 1:
				if !f.inRAGDir {
					// Orphaned file - no preview available
					fileTable.SetCell(r+1, c,
						tview.NewTableCell("not in ragdir").
							SetTextColor(tcell.ColorYellow).
							SetAlign(tview.AlignCenter).
							SetSelectable(false))
				} else if fi, err := os.Stat(f.fullPath); err == nil {
					size := fi.Size()
					modTime := fi.ModTime()
					preview := fmt.Sprintf("%s | %s", formatSize(size), modTime.Format("2006-01-02 15:04"))
					fileTable.SetCell(r+1, c,
						tview.NewTableCell(preview).
							SetTextColor(color).
							SetAlign(tview.AlignCenter).
							SetSelectable(false))
				} else {
					fileTable.SetCell(r+1, c,
						tview.NewTableCell("error").
							SetTextColor(color).
							SetAlign(tview.AlignCenter).
							SetSelectable(false))
				}
			case 2:
				actionText := "load"
				if f.isLoaded {
					actionText = "unload"
				}
				if !f.inRAGDir {
					// Orphaned file - can only unload
					actionText = "unload"
				}
				fileTable.SetCell(r+1, c,
					tview.NewTableCell(actionText).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			case 3:
				if !f.inRAGDir {
					// Orphaned file - cannot delete from ragdir (not there)
					fileTable.SetCell(r+1, c,
						tview.NewTableCell("-").
							SetTextColor(tcell.ColorDarkGray).
							SetAlign(tview.AlignCenter).
							SetSelectable(false))
				} else {
					fileTable.SetCell(r+1, c,
						tview.NewTableCell("delete").
							SetTextColor(color).
							SetAlign(tview.AlignCenter))
				}
			}
		}
	}
	errCh := make(chan error, 1) // why?
	go func() {
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
		SetSelectable(true, true).
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
		tc := fileTable.GetCell(row, column)
		tc.SetTextColor(tcell.ColorRed)
		fileTable.SetSelectable(false, false)
		// Check if the selected row is the exit row (row 0) - do this first to avoid index issues
		if row == 0 {
			pages.RemovePage(RAGPage)
			return
		}
		// For file rows, get the file info (row index - 1 because of the exit row at index 0)
		f := ragFiles[row-1]
		// Handle "-" case (orphaned file with no delete option)
		if tc.Text == "-" {
			return
		}
		switch tc.Text {
		case "load":
			fpath := path.Join(cfg.RAGDir, f.name)
			longStatusView.SetText("clicked load")
			go func() {
				if err := ragger.LoadRAG(fpath); err != nil {
					logger.Error("failed to embed file", "chat", fpath, "error", err)
					showToast("RAG", "failed to embed file; error: "+err.Error())
					return
				}
				showToast("RAG", "file loaded successfully")
				app.QueueUpdate(func() {
					pages.RemovePage(RAGPage)
					loadedFiles, _ := ragger.ListLoaded()
					chatRAGTable := makeRAGTable(fileList, loadedFiles)
					pages.AddPage(RAGPage, chatRAGTable, true, true)
				})
			}()
			return
		case "unload":
			longStatusView.SetText("clicked unload")
			go func() {
				if err := ragger.RemoveFile(f.name); err != nil {
					logger.Error("failed to unload file from RAG", "filename", f.name, "error", err)
					showToast("RAG", "failed to unload file; error: "+err.Error())
					return
				}
				showToast("RAG", "file unloaded successfully")
				app.QueueUpdate(func() {
					pages.RemovePage(RAGPage)
					loadedFiles, _ := ragger.ListLoaded()
					chatRAGTable := makeRAGTable(fileList, loadedFiles)
					pages.AddPage(RAGPage, chatRAGTable, true, true)
				})
			}()
			return
		case "delete":
			fpath := path.Join(cfg.RAGDir, f.name)
			if err := os.Remove(fpath); err != nil {
				logger.Error("failed to delete file", "filename", fpath, "error", err)
				return
			}
			showToast("chat deleted", fpath+" was deleted")
			go func() {
				app.QueueUpdate(func() {
					pages.RemovePage(RAGPage)
					newFileList, _ := os.ReadDir(cfg.RAGDir)
					loadedFiles, _ := ragger.ListLoaded()
					var newFiles []string
					for _, f := range newFileList {
						if !f.IsDir() {
							newFiles = append(newFiles, f.Name())
						}
					}
					chatRAGTable := makeRAGTable(newFiles, loadedFiles)
					pages.AddPage(RAGPage, chatRAGTable, true, true)
				})
			}()
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
		if event.Key() == tcell.KeyRune && event.Rune() == 'd' {
			pages.RemovePage(RAGPage)
			dbTable := makeDbTable()
			if dbTable != nil {
				pages.AddPage(dbTablesPage, dbTable, true, true)
			}
			return nil
		}
		return event
	})
	return ragflex
}

func makeAgentTable(cards []*models.CharCard) *tview.Table {
	actions := []string{"filepath", "load"}
	rows, cols := len(cards), len(actions)+1
	chatActTable := tview.NewTable().
		SetBorders(true)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			color := tcell.ColorWhite
			switch c {
			case 0:
				chatActTable.SetCell(r, c,
					tview.NewTableCell(cards[r].Role).
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			case 1:
				if actions[c-1] == "filepath" {
					chatActTable.SetCell(r, c,
						tview.NewTableCell(cards[r].FilePath).
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
		SetSelectable(true, true).
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
		tc.SetTextColor(tcell.ColorRed)
		chatActTable.SetSelectable(false, false)
		// notification := fmt.Sprintf("chat: %s; action: %s", selectedChat, tc.Text)
		switch tc.Text {
		case "load":
			applyCharCard(cards[row], true)
			// replace textview
			textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
			colorText()
			updateStatusLine()
			pages.RemovePage(agentPage)
			app.SetFocus(textArea)
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
	actions := []string{"copy", "copy with backticks"}
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
		SetSelectable(true, true).
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
		tc.SetTextColor(tcell.ColorRed)
		table.SetSelectable(false, false)
		selected := codeBlocks[row]
		// notification := fmt.Sprintf("chat: %s; action: %s", selectedChat, tc.Text)
		switch tc.Text {
		case "copy":
			// cleanup backticks from selected
			selected = models.CodeBlockLeftRE.ReplaceAllString(selected, "")
			selected = strings.TrimRight(selected, "\n")
			if err := copyToClipboard(selected); err != nil {
				showToast("error", err.Error())
			}
			showToast("copied", selected)
			pages.RemovePage(codeBlockPage)
			app.SetFocus(textArea)
			return
		case "copy with backticks":
			selected = strings.TrimRight(selected, "\n")
			if err := copyToClipboard(selected); err != nil {
				showToast("error", err.Error())
			}
			showToast("copied", selected)
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
		SetSelectable(true, true).
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
			showToast("chat deleted", selected+" was deleted")
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
	// --- NEW: search state ---
	searching := false
	searchQuery := ""
	searchInputMode := false
	// Helper function to check if a file has an allowed extension from config
	hasAllowedExtension := func(filename string) bool {
		if cfg.FilePickerExts == "" {
			return true
		}
		allowedExts := strings.Split(cfg.FilePickerExts, ",")
		lowerFilename := strings.ToLower(strings.TrimSpace(filename))
		for _, ext := range allowedExts {
			ext = strings.TrimSpace(ext)
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
	listView.SetBorder(true).
		SetTitle("Files & Directories [s: set FilePickerDir]. Current base dir: " + cfg.FilePickerDir).
		SetTitleAlign(tview.AlignLeft)
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
	// Pending images list
	pendingImagesView := tview.NewTextView()
	pendingImagesView.SetBorder(true).SetTitle("Pending Images [d: remove last]").SetTitleAlign(tview.AlignLeft)
	pendingImagesView.SetTextColor(tcell.ColorOrange)
	pendingImagesView.SetTextAlign(tview.AlignLeft)
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
	// Helper to update pending images display
	updatePendingImagesView := func() {
		if len(pendingImageAttachments) == 0 {
			pendingImagesView.SetText("(none)")
		} else {
			var lines []string
			for i, p := range pendingImageAttachments {
				lines = append(lines, fmt.Sprintf("%d. %s", i+1, path.Base(p)))
			}
			pendingImagesView.SetText(strings.Join(lines, "\n"))
		}
	}
	updatePendingImagesView()
	// Main vertical flex
	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(hFlex, 0, 3, true)
	flex.AddItem(pendingImagesView, 5, 0, false)
	flex.AddItem(statusView, 3, 0, false)
	// Refresh the file list – now accepts a filter string
	var refreshList func(string, string)
	refreshList = func(dir string, filter string) {
		listView.Clear()
		// Update the current display directory
		currentDisplayDir = dir
		// Add exit option at the top
		listView.AddItem("Exit file picker [gray](Close without selecting)[-]", "", 'x', func() {
			pages.RemovePage(filePickerPage)
		})
		// Add parent directory (..) if not at root
		if dir != "/" {
			parentDir := path.Dir(dir)
			// For Unix-like systems, avoid infinite loop when at root
			if parentDir != dir {
				listView.AddItem("../ [gray](Parent Directory)[-]", "", 'p', func() {
					// Clear search on navigation
					searching = false
					searchQuery = ""
					if currentFilePickerUeberzugImg != nil {
						currentFilePickerUeberzugImg.Destroy()
						currentFilePickerUeberzugImg = nil
					}
					if cfg.ImagePreview {
						imgPreview.SetImage(nil)
					}
					refreshList(parentDir, "")
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
		// Helper to check if an item passes the filter
		matchesFilter := func(name string) bool {
			if filter == "" {
				return true
			}
			return strings.Contains(strings.ToLower(name), strings.ToLower(filter))
		}
		// Add directories
		for _, file := range files {
			name := file.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if file.IsDir() && matchesFilter(name) {
				dirName := name
				listView.AddItem(dirName+"/ [gray](Directory)[-]", "", 0, func() {
					// Clear search on navigation
					searching = false
					searchQuery = ""
					if currentFilePickerUeberzugImg != nil {
						currentFilePickerUeberzugImg.Destroy()
						currentFilePickerUeberzugImg = nil
					}
					if cfg.ImagePreview {
						imgPreview.SetImage(nil)
					}
					newDir := path.Join(dir, dirName)
					refreshList(newDir, "")
					dirStack = append(dirStack, newDir)
					currentStackPos = len(dirStack) - 1
					statusView.SetText("Current: " + newDir)
				})
			}
		}
		// Add files with allowed extensions
		for _, file := range files {
			name := file.Name()
			if strings.HasPrefix(name, ".") || file.IsDir() {
				continue
			}
			if hasAllowedExtension(name) && matchesFilter(name) {
				fileName := name
				fullFilePath := path.Join(dir, fileName)
				listView.AddItem(fileName+" [gray](File)[-]", "", 0, func() {
					selectedFile = fullFilePath
					statusView.SetText("Selected: " + selectedFile)
					if isImageFile(fileName) {
						statusView.SetText("Selected image: " + selectedFile)
					}
				})
			}
		}
		// Update status line based on search state
		switch {
		case searching:
			statusView.SetText("Search: " + searchQuery + "_")
		case searchQuery != "":
			statusView.SetText("Current: " + dir + " (filter: " + searchQuery + ")")
		default:
			statusView.SetText("Current: " + dir)
		}
	}
	// Initialize the file list
	refreshList(startDir, "")
	// Update image preview when selection changes
	if cfg.ImagePreview && imgPreview != nil {
		listView.SetChangedFunc(func(index int, mainText, secondaryText string, rune rune) {
			if currentFilePickerUeberzugImg != nil {
				currentFilePickerUeberzugImg.Destroy()
				currentFilePickerUeberzugImg = nil
			}
			itemText, _ := listView.GetItemText(index)
			if strings.HasPrefix(itemText, "Exit file picker") || strings.HasPrefix(itemText, "../") {
				if imgPreview != nil {
					imgPreview.SetImage(nil)
				}
				return
			}
			actualItemName := itemText
			if bracketPos := strings.Index(itemText, " ["); bracketPos != -1 {
				actualItemName = itemText[:bracketPos]
			}
			if strings.HasSuffix(actualItemName, "/") {
				if imgPreview != nil {
					imgPreview.SetImage(nil)
				}
				return
			}
			if !isImageFile(actualItemName) {
				if imgPreview != nil {
					imgPreview.SetImage(nil)
				}
				return
			}
			filePath := path.Join(currentDisplayDir, actualItemName)
			file, err := os.Open(filePath)
			if err != nil {
				if imgPreview != nil {
					imgPreview.SetImage(nil)
				}
				return
			}
			defer file.Close()
			imgData, _, err := image.Decode(file)
			if err != nil {
				if imgPreview != nil {
					imgPreview.SetImage(nil)
				}
				return
			}
			if ueberzugAvailable {
				cellX, cellY, cellW, cellH := 0, 0, 0, 0
				if imgPreview != nil {
					cellX, cellY, cellW, cellH = imgPreview.GetRect()
				}
				if cellW == 0 || cellH == 0 {
					if imgPreview != nil {
						imgPreview.SetImage(imgData)
					}
					return
				}

geom, err := getTerminalGeometry()
			if err != nil {
				logger.Warn("ueberzug fallback: getTerminalGeometry failed", "error", err)
				if imgPreview != nil {
					imgPreview.SetImage(imgData)
				}
				return
			}

			x, y := cellToPixel(cellX, cellY, geom)

			maxSize := 500
			scaledImg := resize.Resize(0, uint(maxSize), imgData, resize.Lanczos3)

			uimg, err := ueberzug.NewImage(scaledImg, x, y)
			if err != nil {
				logger.Warn("ueberzug fallback: NewImage failed", "error", err, "x", x, "y", y)
				if imgPreview != nil {
						imgPreview.SetImage(imgData)
					}
					return
				}
				currentFilePickerUeberzugImg = uimg
				if imgPreview != nil {
					imgPreview.SetImage(nil)
				}
			} else if imgPreview != nil {
				imgPreview.SetImage(imgData)
			}
		})
	}
	// Set up keyboard navigation
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// --- Handle search mode ---
		if searching {
			switch event.Key() {
			case tcell.KeyEsc:
				// Exit search, clear filter
				searching = false
				searchInputMode = false
				searchQuery = ""
				refreshList(currentDisplayDir, "")
				return nil
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				if len(searchQuery) > 0 {
					searchQuery = searchQuery[:len(searchQuery)-1]
					refreshList(currentDisplayDir, searchQuery)
				}
				return nil
			case tcell.KeyEnter:
				// Exit search input mode and let normal processing handle selection
				searchInputMode = false
				// Get the currently highlighted item in the list
				itemIndex := listView.GetCurrentItem()
				if itemIndex >= 0 && itemIndex < listView.GetItemCount() {
					itemText, _ := listView.GetItemText(itemIndex)
					// Check for the exit option first
					if strings.HasPrefix(itemText, "Exit file picker") {
						if currentFilePickerUeberzugImg != nil {
							currentFilePickerUeberzugImg.Destroy()
							currentFilePickerUeberzugImg = nil
						}
						pages.RemovePage(filePickerPage)
						return nil
					}
					// Extract the actual filename/directory name by removing the type info
					actualItemName := itemText
					if bracketPos := strings.Index(itemText, " ["); bracketPos != -1 {
						actualItemName = itemText[:bracketPos]
					}
					// Check if it's a directory (ends with /)
					if strings.HasSuffix(actualItemName, "/") {
						var targetDir string
						if strings.HasPrefix(actualItemName, "../") {
							// Parent directory
							targetDir = path.Dir(currentDisplayDir)
							if targetDir == currentDisplayDir && currentDisplayDir == "/" {
								return nil
							}
						} else {
							// Regular subdirectory
							dirName := strings.TrimSuffix(actualItemName, "/")
							targetDir = path.Join(currentDisplayDir, dirName)
						}
						// Navigate – clear search
						if currentFilePickerUeberzugImg != nil {
							currentFilePickerUeberzugImg.Destroy()
							currentFilePickerUeberzugImg = nil
						}
						if cfg.ImagePreview && imgPreview != nil {
							imgPreview.SetImage(nil)
						}
						searching = false
						searchInputMode = false
						searchQuery = ""
						refreshList(targetDir, "")
						dirStack = append(dirStack, targetDir)
						currentStackPos = len(dirStack) - 1
						statusView.SetText("Current: " + targetDir)
						return nil
				} else {
					// It's a file
					filePath := path.Join(currentDisplayDir, actualItemName)
					if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
						if isImageFile(actualItemName) {
							if currentFilePickerUeberzugImg != nil {
								currentFilePickerUeberzugImg.Destroy()
								currentFilePickerUeberzugImg = nil
							}
							AddImageAttachment(filePath)
							updatePendingImagesView()
							statusView.SetText("Image added: " + path.Base(filePath) + " (total: " + strconv.Itoa(len(pendingImageAttachments)) + ")")
						} else {
							if currentFilePickerUeberzugImg != nil {
								currentFilePickerUeberzugImg.Destroy()
								currentFilePickerUeberzugImg = nil
							}
							textArea.SetText(filePath, true)
							app.SetFocus(textArea)
							pages.RemovePage(filePickerPage)
						}
					}
					return nil
				}
				}
				return nil
			case tcell.KeyRune:
				r := event.Rune()
				if searchInputMode && r != 0 {
					searchQuery += string(r)
					refreshList(currentDisplayDir, searchQuery)
					return nil
				}
				// Handle 'd' to remove last image even while searching
				if r == 'd' {
					if len(pendingImageAttachments) > 0 {
						removed := pendingImageAttachments[len(pendingImageAttachments)-1]
						RemoveLastImageAttachment()
						updatePendingImagesView()
						if len(pendingImageAttachments) == 0 {
							statusView.SetText("Removed last pending image: " + path.Base(removed))
						} else {
							statusView.SetText("Removed: " + path.Base(removed) + " (remaining: " + strconv.Itoa(len(pendingImageAttachments)) + ")")
						}
					} else {
						statusView.SetText("No pending images to remove")
					}
					return nil
				}
				// If not in search input mode, pass through for navigation
				return event
			default:
				// Exit search input mode but keep filter active for navigation
				searchInputMode = false
				// Pass all other keys (arrows, etc.) to normal processing
				return event
			}
		}
		// --- Not searching ---
		switch event.Key() {
		case tcell.KeyEsc:
			if currentFilePickerUeberzugImg != nil {
				currentFilePickerUeberzugImg.Destroy()
				currentFilePickerUeberzugImg = nil
			}
			pages.RemovePage(filePickerPage)
			return nil
		case tcell.KeyBackspace2: // Backspace to go to parent directory
			if currentFilePickerUeberzugImg != nil {
				currentFilePickerUeberzugImg.Destroy()
				currentFilePickerUeberzugImg = nil
			}
			if cfg.ImagePreview && imgPreview != nil {
				imgPreview.SetImage(nil)
			}
			if currentStackPos > 0 {
				currentStackPos--
				prevDir := dirStack[currentStackPos]
				// Clear search when navigating with backspace
				searching = false
				searchQuery = ""
				refreshList(prevDir, "")
				// Trim the stack to current position
				dirStack = dirStack[:currentStackPos+1]
			}
			return nil
		case tcell.KeyRune:
			if event.Rune() == '/' {
				// Enter search mode
				searching = true
				searchInputMode = true
				searchQuery = ""
				refreshList(currentDisplayDir, "")
				return nil
			}
			if event.Rune() == 's' {
				// Set FilePickerDir to current directory
				// Get the actual directory path
				cfg.FilePickerDir = currentDisplayDir
				listView.SetTitle("Files & Directories [s: set FilePickerDir]. Current base dir: " + cfg.FilePickerDir)
				// pages.RemovePage(filePickerPage)
				return nil
			}
			if event.Rune() == 'd' {
				// Remove last pending image
				if len(pendingImageAttachments) > 0 {
					removed := pendingImageAttachments[len(pendingImageAttachments)-1]
					RemoveLastImageAttachment()
					updatePendingImagesView()
					if len(pendingImageAttachments) == 0 {
						statusView.SetText("Removed last pending image: " + path.Base(removed))
					} else {
						statusView.SetText("Removed: " + path.Base(removed) + " (remaining: " + strconv.Itoa(len(pendingImageAttachments)) + ")")
					}
				} else {
					statusView.SetText("No pending images to remove")
				}
				return nil
			}
		case tcell.KeyEnter:
			// Get the currently highlighted item in the list
			itemIndex := listView.GetCurrentItem()
			if itemIndex >= 0 && itemIndex < listView.GetItemCount() {
				itemText, _ := listView.GetItemText(itemIndex)
				logger.Info("choosing dir", "itemText", itemText)
				// Check for the exit option first
				if strings.HasPrefix(itemText, "Exit file picker") {
					pages.RemovePage(filePickerPage)
					return nil
				}
				// Extract the actual filename/directory name by removing the type info
				actualItemName := itemText
				if bracketPos := strings.Index(itemText, " ["); bracketPos != -1 {
					actualItemName = itemText[:bracketPos]
				}
				// Check if it's a directory (ends with /)
				if strings.HasSuffix(actualItemName, "/") {
					var targetDir string
					if strings.HasPrefix(actualItemName, "../") {
						// Parent directory
						targetDir = path.Dir(currentDisplayDir)
						if targetDir == currentDisplayDir && currentDisplayDir == "/" {
							logger.Warn("at root, cannot go up")
							return nil
						}
					} else {
						// Regular subdirectory
						dirName := strings.TrimSuffix(actualItemName, "/")
						targetDir = path.Join(currentDisplayDir, dirName)
					}
					// Navigate – clear search
					logger.Info("going to dir", "dir", targetDir)
					if currentFilePickerUeberzugImg != nil {
						currentFilePickerUeberzugImg.Destroy()
						currentFilePickerUeberzugImg = nil
					}
					if cfg.ImagePreview && imgPreview != nil {
						imgPreview.SetImage(nil)
					}
					searching = false
					searchQuery = ""
					refreshList(targetDir, "")
					dirStack = append(dirStack, targetDir)
					currentStackPos = len(dirStack) - 1
					statusView.SetText("Current: " + targetDir)
					return nil
			} else {
				// It's a file
				filePath := path.Join(currentDisplayDir, actualItemName)
				if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
					if isImageFile(actualItemName) {
						if currentFilePickerUeberzugImg != nil {
							currentFilePickerUeberzugImg.Destroy()
							currentFilePickerUeberzugImg = nil
						}
						logger.Info("adding image", "file", actualItemName)
						AddImageAttachment(filePath)
						updatePendingImagesView()
						logger.Info("after adding image", "file", actualItemName, "count", len(pendingImageAttachments))
						statusView.SetText("Image added: " + path.Base(filePath) + " (total: " + strconv.Itoa(len(pendingImageAttachments)) + ")")
						logger.Info("after setting text", "file", actualItemName)
					} else {
						if currentFilePickerUeberzugImg != nil {
							currentFilePickerUeberzugImg.Destroy()
							currentFilePickerUeberzugImg = nil
						}
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

func makeDbTable() *tview.Flex {
	tables, err := store.ListTables()
	if err != nil {
		logger.Error("failed to list tables", "error", err)
		showToast("error", "failed to list tables: "+err.Error())
		return nil
	}
	if len(tables) == 0 {
		showToast("info", "no tables found in database")
		return nil
	}
	tblList := tview.NewList().ShowSecondaryText(false)
	rowCounts := make(map[string]int)
	for _, t := range tables {
		var count int
		_ = store.DB().Get(&count, "SELECT COUNT(*) FROM "+t)
		rowCounts[t] = count
		tblList.AddItem(t, fmt.Sprintf("%d rows", count), 0, nil)
	}
	tblList.SetBorder(true).SetTitle("Tables")
	dataTable := tview.NewTable().SetBorders(true)
	dataTable.SetBorder(true).SetTitle("Data")
	flex := tview.NewFlex().
		AddItem(tblList, 0, 1, true).
		AddItem(dataTable, 0, 2, false)
	loadTableData := func(tableName string, tbl *tview.Table) {
		rows, err := store.DB().Queryx("SELECT * FROM " + tableName + " LIMIT 80")
		if err != nil {
			logger.Error("failed to query table", "table", tableName, "error", err)
			return
		}
		columnNames, _ := rows.Columns()
		tbl.Clear()
		for c, name := range columnNames {
			tbl.SetCell(0, c,
				tview.NewTableCell(name).
					SetTextColor(tcell.ColorYellow).
					SetAlign(tview.AlignCenter))
		}
		r := 1
		for rows.Next() {
			row := make(map[string]interface{})
			if err := rows.MapScan(row); err != nil {
				continue
			}
			for c, name := range columnNames {
				val, ok := row[name]
				var cellText string
				var color tcell.Color
				if !ok || val == nil {
					cellText = "NULL"
					color = tcell.ColorDarkGray
				} else {
					cellText = fmt.Sprintf("%v", val)
					if len(cellText) > 30 {
						cellText = cellText[:30] + "..."
					}
					color = tcell.ColorWhite
				}
				tbl.SetCell(r, c,
					tview.NewTableCell(cellText).
						SetTextColor(color).
						SetAlign(tview.AlignCenter))
			}
			r++
		}
		rows.Close()
		tbl.Select(0, 0)
	}
	tblList.SetSelectedFunc(func(idx int, mainText, secondaryText string, rune rune) {
		if idx >= 0 && idx < len(tables) {
			loadTableData(tables[idx], dataTable)
			dataTable.SetBorder(true).SetTitle("Data: " + tables[idx])
		}
	})
	tblList.SetChangedFunc(func(idx int, mainText, secondaryText string, rune rune) {
		if idx >= 0 && idx < len(tables) {
			loadTableData(tables[idx], dataTable)
			dataTable.SetBorder(true).SetTitle("Data: " + tables[idx])
		}
	})
	tblList.SetDoneFunc(func() {
		pages.RemovePage(dbTablesPage)
	})
	tblList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(dbTablesPage)
			app.SetFocus(textArea)
			return nil
		}
		if event.Key() == tcell.KeyEnter {
			idx := tblList.GetCurrentItem()
			if idx >= 0 && idx < len(tables) {
				showDbContentView(tables[idx])
			}
			return nil
		}
		return event
	})
	if len(tables) > 0 {
		tblList.SetCurrentItem(0)
	}
	return flex
}

func showDbContentView(tableName string) {
	batchSize := 80
	longStatusView := tview.NewTextView()
	longStatusView.SetText("table: " + tableName + " | press Enter to load more").SetBorder(true).SetTitle("status")
	longStatusView.SetChangedFunc(func() {
		app.Draw()
	})
	tbl := tview.NewTable().SetBorders(true).SetFixed(1, 0)
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(longStatusView, 0, 10, false).
		AddItem(tbl, 0, 60, true)
	contentPageName := "db_content_" + tableName
	offset := 0
	var rowCount int
	_ = store.DB().Get(&rowCount, "SELECT COUNT(*) FROM "+tableName)
	var columnNames []string
	loadRows := func(off int) {
		rows, err := store.DB().Queryx("SELECT * FROM " + tableName + " LIMIT " + strconv.Itoa(batchSize) + " OFFSET " + strconv.Itoa(off))
		if err != nil {
			logger.Error("failed to query table", "table", tableName, "error", err)
			return
		}
		if off == 0 {
			columnNames, _ = rows.Columns()
			for c, name := range columnNames {
				tbl.SetCell(0, c,
					tview.NewTableCell(name).
						SetTextColor(tcell.ColorYellow).
						SetAlign(tview.AlignCenter).
						SetSelectable(false))
			}
		}
		r := off
		for rows.Next() {
			row := make(map[string]interface{})
			if err := rows.MapScan(row); err != nil {
				logger.Error("failed to scan row", "error", err)
				continue
			}
			for c, name := range columnNames {
				val, ok := row[name]
				if !ok {
					tbl.SetCell(r+1, c,
						tview.NewTableCell("NULL").
							SetTextColor(tcell.ColorDarkGray).
							SetAlign(tview.AlignCenter).
							SetSelectable(false))
				} else {
					str := fmt.Sprintf("%v", val)
					if len(str) > 50 {
						str = str[:50] + "..."
					}
					tbl.SetCell(r+1, c,
						tview.NewTableCell(str).
							SetTextColor(tcell.ColorWhite).
							SetAlign(tview.AlignCenter).
							SetSelectable(false))
				}
			}
			r++
		}
		rows.Close()
		loaded := tbl.GetRowCount() - 1
		if loaded < rowCount {
			longStatusView.SetText(fmt.Sprintf("table: %s | loaded %d of %d rows | press Enter for more", tableName, loaded, rowCount))
		} else {
			longStatusView.SetText(fmt.Sprintf("table: %s | loaded %d rows (all)", tableName, loaded))
		}
	}
	loadRows(0)
	pages.AddPage(contentPageName, flex, true, true)
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(contentPageName)
			return nil
		}
		if event.Key() == tcell.KeyEnter {
			offset += batchSize
			loadRows(offset)
			tbl.ScrollToEnd()
		}
		return event
	})
}

type chatImageEntry struct {
	path     string
	msgIndex int
	role     string
	hasFile  bool
}

func extractChatImages(messages []models.RoleMsg) []chatImageEntry {
	var result []chatImageEntry
	for i := range messages {
		msg := &messages[i]
		if !msg.HasContentParts {
			continue
		}
		for _, part := range msg.ContentParts {
			var displayPath string
			switch p := part.(type) {
			case models.ImageContentPart:
				displayPath = p.Path
			case map[string]any:
				if partType, exists := p["type"]; exists && partType == "image_url" {
					if pathVal, pathExists := p["path"]; pathExists {
						if pathStr, isStr := pathVal.(string); isStr {
							displayPath = pathStr
						}
					}
				} else {
					continue
				}
			default:
				continue
			}
			hasFile := displayPath != "" && tools.IsImageFile(displayPath)
			if hasFile {
				if _, err := os.Stat(displayPath); err != nil {
					hasFile = false
				}
			}
			result = append(result, chatImageEntry{
				path:     displayPath,
				msgIndex: i,
				role:     msg.Role,
				hasFile:  hasFile,
			})
		}
	}
	return result
}

func makeImagesTable() *tview.Flex {
	images := extractChatImages(chatBody.Messages)
	if len(images) == 0 {
		showToast("info", "no images in current chat")
		return nil
	}
	listView := tview.NewList()
	listView.SetBorder(true).SetTitle(fmt.Sprintf("Chat Images (%d)", len(images))).SetTitleAlign(tview.AlignLeft)
	statusView := tview.NewTextView()
	statusView.SetBorder(true).SetTitle("Info").SetTitleAlign(tview.AlignLeft)
	statusView.SetTextColor(tcell.ColorYellow)
	var imgPreview *tview.Image
	if cfg.ImagePreview {
		imgPreview = tview.NewImage()
		imgPreview.SetBorder(true).SetTitle("Preview").SetTitleAlign(tview.AlignLeft)
	}
	var hFlex *tview.Flex
	if cfg.ImagePreview && imgPreview != nil {
		hFlex = tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(listView, 0, 3, true).
			AddItem(imgPreview, 0, 2, false)
	} else {
		hFlex = tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(listView, 0, 1, true)
	}
	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(hFlex, 0, 3, true)
	flex.AddItem(statusView, 3, 0, false)
	var currentUeberzugImg *ueberzug.Image
	cleanupUeberzug := func() {
		if currentUeberzugImg != nil {
			currentUeberzugImg.Destroy()
			currentUeberzugImg = nil
		}
	}
	loadImagePreview := func(entryPath string) {
		cleanupUeberzug()
		if imgPreview == nil && !ueberzugAvailable {
			return
		}
		if !tools.IsImageFile(entryPath) {
			if imgPreview != nil {
				imgPreview.SetImage(nil)
			}
			return
		}
		file, err := os.Open(entryPath)
		if err != nil {
			if imgPreview != nil {
				imgPreview.SetImage(nil)
			}
			return
		}
		defer file.Close()
		imgData, _, err := image.Decode(file)
		if err != nil {
			if imgPreview != nil {
				imgPreview.SetImage(nil)
			}
			return
		}
		if ueberzugAvailable {
			cellX, cellY, cellW, cellH := 0, 0, 0, 0
			if imgPreview != nil {
				cellX, cellY, cellW, cellH = imgPreview.GetRect()
			}
			if cellW == 0 || cellH == 0 {
				if imgPreview != nil {
					imgPreview.SetImage(imgData)
				}
				return
			}

			geom, err := getTerminalGeometry()
			if err != nil {
				logger.Warn("ueberzug fallback: getTerminalGeometry failed", "error", err)
				if imgPreview != nil {
					imgPreview.SetImage(imgData)
				}
				return
			}

			x, y := cellToPixel(cellX, cellY, geom)

			maxSize := 500
			scaledImg := resize.Resize(0, uint(maxSize), imgData, resize.Lanczos3)

			uimg, err := ueberzug.NewImage(scaledImg, x, y)
			if err != nil {
				logger.Warn("ueberzug fallback: NewImage failed", "error", err, "x", x, "y", y)
				if imgPreview != nil {
					imgPreview.SetImage(imgData)
				}
				return
			}
			currentUeberzugImg = uimg
			if imgPreview != nil {
				imgPreview.SetImage(nil)
			}
		} else if imgPreview != nil {
			imgPreview.SetImage(imgData)
		}
	}
	for i, img := range images {
		displayName := extractDisplayPath(img.path, cfg.FilePickerDir)
		if displayName == "" {
			displayName = fmt.Sprintf("image #%d (data-only, no file)", i)
		}
		label := fmt.Sprintf("%s (msg #%d, %s)", displayName, img.msgIndex, img.role)
		if !img.hasFile {
			label += " [gray](file not found)[-]"
		}
		listView.AddItem(label, "", 0, nil)
	}
	listView.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		if index < 0 || index >= len(images) {
			return
		}
		entry := images[index]
		statusView.SetText(fmt.Sprintf("Path: %s\nMessage: #%d (%s)\nFile exists: %v",
			entry.path, entry.msgIndex, entry.role, entry.hasFile))
		if entry.hasFile {
			loadImagePreview(entry.path)
		} else {
			cleanupUeberzug()
			if imgPreview != nil {
				imgPreview.SetImage(nil)
			}
		}
	})
	flex.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		flex.SetDrawFunc(nil)
		go func() {
			time.Sleep(50 * time.Millisecond)
			if len(images) > 0 && images[0].hasFile {
				loadImagePreview(images[0].path)
			}
		}()
		return x, y, w, h
	})
	closeImagesTable := func() {
		cleanupUeberzug()
		pages.RemovePage(imagesPage)
	}
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyF1:
			closeImagesTable()
			return nil
		case tcell.KeyRune:
			if event.Rune() == 'x' {
				closeImagesTable()
				return nil
			}
		case tcell.KeyEnter:
			itemIndex := listView.GetCurrentItem()
			if itemIndex >= 0 && itemIndex < len(images) {
				entry := images[itemIndex]
				if entry.hasFile {
					cleanupUeberzug()
					SetImageAttachment(entry.path)
					showToast("image attached", entry.path)
					pages.RemovePage(imagesPage)
					app.SetFocus(textArea)
				} else {
					showToast("error", "cannot attach: file not found on disk")
				}
			}
			return nil
		}
		return event
	})
	return flex
}
