package main

import (
	"fmt"
	"gf-lt/models"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var _ = sync.RWMutex{}

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

=== scrolling chat window (some keys similar to vim) ===
[yellow]arrows up/down and j/k[white]: scroll up and down
[yellow]gg/G[white]: jump to the begging / end of the chat
[yellow]/[white]: start searching for text
[yellow]n[white]: go to next search result
[yellow]N[white]: go to previous search result

=== tables (chat history, agent pick, file pick, properties) ===
[yellow]x[white]: to exit the table page

=== status line ===
%s

Press <Enter> or 'x' to return
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

func toggleShellMode() {
	shellMode = !shellMode
	if shellMode {
		// Update input placeholder to indicate shell mode
		textArea.SetPlaceholder("SHELL MODE: Enter command and press <Esc> to execute")
	} else {
		// Reset to normal mode
		textArea.SetPlaceholder("input is multiline; press <Enter> to start the next line;\npress <Esc> to send the message. Alt+1 to exit shell mode")
	}
	updateStatusLine()
}

func updateFlexLayout() {
	if fullscreenMode {
		// flex already contains only focused widget; do nothing
		return
	}
	flex.Clear()
	flex.AddItem(textView, 0, 40, false)
	flex.AddItem(textArea, 0, 10, false)
	if positionVisible {
		flex.AddItem(statusLineWidget, 0, 2, false)
	}
	// Keep focus on currently focused widget
	focused := app.GetFocus()
	if focused == textView {
		app.SetFocus(textView)
	} else {
		app.SetFocus(textArea)
	}
}

func executeCommandAndDisplay(cmdText string) {
	// Parse the command (split by spaces, but handle quoted arguments)
	cmdParts := parseCommand(cmdText)
	if len(cmdParts) == 0 {
		fmt.Fprintf(textView, "\n[red]Error: No command provided[-:-:-]\n")
		if scrollToEndEnabled {
			textView.ScrollToEnd()
		}
		colorText()
		return
	}
	command := cmdParts[0]
	args := []string{}
	if len(cmdParts) > 1 {
		args = cmdParts[1:]
	}
	// Create the command execution
	cmd := exec.Command(command, args...)
	// Execute the command and get output
	output, err := cmd.CombinedOutput()
	// Add the command being executed to the chat
	fmt.Fprintf(textView, "\n[yellow]$ %s[-:-:-]\n", cmdText)
	var outputContent string
	if err != nil {
		// Include both output and error
		errorMsg := "Error: " + err.Error()
		fmt.Fprintf(textView, "[red]%s[-:-:-]\n", errorMsg)
		if len(output) > 0 {
			outputStr := string(output)
			fmt.Fprintf(textView, "[red]%s[-:-:-]\n", outputStr)
			outputContent = errorMsg + "\n" + outputStr
		} else {
			outputContent = errorMsg
		}
	} else {
		// Only output if successful
		if len(output) > 0 {
			outputStr := string(output)
			fmt.Fprintf(textView, "[green]%s[-:-:-]\n", outputStr)
			outputContent = outputStr
		} else {
			successMsg := "Command executed successfully (no output)"
			fmt.Fprintf(textView, "[green]%s[-:-:-]\n", successMsg)
			outputContent = successMsg
		}
	}
	// Combine command and output in a single message for chat history
	combinedContent := "$ " + cmdText + "\n\n" + outputContent
	combinedMsg := models.RoleMsg{
		Role:    cfg.ToolRole,
		Content: combinedContent,
	}
	chatBody.Messages = append(chatBody.Messages, combinedMsg)
	// Scroll to end and update colors
	if scrollToEndEnabled {
		textView.ScrollToEnd()
	}
	colorText()
}

// parseCommand splits command string handling quotes properly
func parseCommand(cmd string) []string {
	var args []string
	var current string
	var inQuotes bool
	var quoteChar rune
	for _, r := range cmd {
		switch r {
		case '"', '\'':
			if inQuotes {
				if r == quoteChar {
					inQuotes = false
				} else {
					current += string(r)
				}
			} else {
				inQuotes = true
				quoteChar = r
			}
		case ' ', '\t':
			if inQuotes {
				current += string(r)
			} else if current != "" {
				args = append(args, current)
				current = ""
			}
		default:
			current += string(r)
		}
	}
	if current != "" {
		args = append(args, current)
	}
	return args
}

// Global variables for search state
var searchResults []int
var searchResultLengths []int // To store the length of each match in the formatted string
var searchIndex int
var searchText string
var originalTextForSearch string

// performSearch searches for the given term in the textView content and highlights matches
func performSearch(term string) {
	searchText = term
	if searchText == "" {
		searchResults = nil
		searchResultLengths = nil
		originalTextForSearch = ""
		// Re-render text without highlights
		textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
		colorText()
		return
	}
	// Get formatted text and search directly in it to avoid mapping issues
	formattedText := textView.GetText(true)
	originalTextForSearch = formattedText
	searchTermLower := strings.ToLower(searchText)
	formattedTextLower := strings.ToLower(formattedText)
	// Find all occurrences of the search term in the formatted text directly
	formattedSearchResults := []int{}
	searchStart := 0
	for {
		pos := strings.Index(formattedTextLower[searchStart:], searchTermLower)
		if pos == -1 {
			break
		}
		absolutePos := searchStart + pos
		formattedSearchResults = append(formattedSearchResults, absolutePos)
		searchStart = absolutePos + len(searchText)
	}
	if len(formattedSearchResults) == 0 {
		// No matches found
		searchResults = nil
		searchResultLengths = nil
		notification := "Pattern not found: " + term
		if err := notifyUser("search", notification); err != nil {
			logger.Error("failed to send notification", "error", err)
		}
		return
	}
	// Store the formatted text positions and lengths for accurate highlighting
	searchResults = formattedSearchResults
	// Create lengths array - all matches have the same length as the search term
	searchResultLengths = make([]int, len(formattedSearchResults))
	for i := range searchResultLengths {
		searchResultLengths[i] = len(searchText)
	}
	searchIndex = 0
	highlightCurrentMatch()
}

// highlightCurrentMatch highlights the current search match and scrolls to it
func highlightCurrentMatch() {
	if len(searchResults) == 0 || searchIndex >= len(searchResults) {
		return
	}
	// Get the stored formatted text
	formattedText := originalTextForSearch
	// For tview to properly support highlighting and scrolling, we need to work with its region system
	// Instead of just applying highlights, we need to add region tags to the text
	highlightedText := addRegionTags(formattedText, searchResults, searchResultLengths, searchIndex, searchText)
	// Update the text view with the text that includes region tags
	textView.SetText(highlightedText)
	// Highlight the current region and scroll to it
	// Need to identify which position in the results array corresponds to the current match
	// The region ID will be search_<position>_<index>
	currentRegion := fmt.Sprintf("search_%d_%d", searchResults[searchIndex], searchIndex)
	textView.Highlight(currentRegion).ScrollToHighlight()
	// Send notification about which match we're at
	notification := fmt.Sprintf("Match %d of %d", searchIndex+1, len(searchResults))
	if err := notifyUser("search", notification); err != nil {
		logger.Error("failed to send notification", "error", err)
	}
}

// showSearchBar shows the search input field as an overlay
func showSearchBar() {
	// Create a temporary flex to combine search and main content
	updatedFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(searchField, 3, 0, true). // Search field at top
		AddItem(flex, 0, 1, false)        // Main flex layout below

	// Add the search overlay as a page
	pages.AddPage(searchPageName, updatedFlex, true, true)
	app.SetFocus(searchField)
}

// hideSearchBar hides the search input field
func hideSearchBar() {
	pages.RemovePage(searchPageName)
	// Return focus to the text view
	app.SetFocus(textView)
	// Clear the search field
	searchField.SetText("")
}

// Global variables for index overlay functionality
var indexPageName = "indexOverlay"

// showIndexBar shows the index input field as an overlay at the top
func showIndexBar() {
	// Create a temporary flex to combine index input and main content
	updatedFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(indexPickWindow, 3, 0, true). // Index field at top
		AddItem(flex, 0, 1, false)            // Main flex layout below

	// Add the index overlay as a page
	pages.AddPage(indexPageName, updatedFlex, true, true)
	app.SetFocus(indexPickWindow)
}

// hideIndexBar hides the index input field
func hideIndexBar() {
	pages.RemovePage(indexPageName)
	// Return focus to the text view
	app.SetFocus(textView)
	// Clear the index field
	indexPickWindow.SetText("")
}

// addRegionTags adds region tags to search matches in the text for tview highlighting
func addRegionTags(text string, positions []int, lengths []int, currentIdx int, searchTerm string) string {
	if len(positions) == 0 {
		return text
	}
	var result strings.Builder
	lastEnd := 0
	for i, pos := range positions {
		endPos := pos + lengths[i]
		// Add text before this match
		if pos > lastEnd {
			result.WriteString(text[lastEnd:pos])
		}
		// The matched text, which may contain its own formatting tags
		actualText := text[pos:endPos]
		// Add region tag and highlighting for this match
		// Use a unique region id that includes the match index to avoid conflicts
		regionId := fmt.Sprintf("search_%d_%d", pos, i) // position + index to ensure uniqueness
		var highlightStart, highlightEnd string
		if i == currentIdx {
			// Current match - use different highlighting
			highlightStart = fmt.Sprintf(`["%s"][yellow:blue:b]`, regionId) // Current match with region and special highlight
			highlightEnd = `[-:-:-][""]`                                    // Reset formatting and close region
		} else {
			// Other matches - use regular highlighting
			highlightStart = fmt.Sprintf(`["%s"][gold:red:u]`, regionId) // Other matches with region and highlight
			highlightEnd = `[-:-:-][""]`                                 // Reset formatting and close region
		}
		result.WriteString(highlightStart)
		result.WriteString(actualText)
		result.WriteString(highlightEnd)
		lastEnd = endPos
	}
	// Add the rest of the text after the last processed match
	if lastEnd < len(text) {
		result.WriteString(text[lastEnd:])
	}
	return result.String()
}

// searchNext finds the next occurrence of the search term
func searchNext() {
	if len(searchResults) == 0 {
		if err := notifyUser("search", "No search results to navigate"); err != nil {
			logger.Error("failed to send notification", "error", err)
		}
		return
	}
	searchIndex = (searchIndex + 1) % len(searchResults)
	highlightCurrentMatch()
}

// searchPrev finds the previous occurrence of the search term
func searchPrev() {
	if len(searchResults) == 0 {
		if err := notifyUser("search", "No search results to navigate"); err != nil {
			logger.Error("failed to send notification", "error", err)
		}
		return
	}
	if searchIndex == 0 {
		searchIndex = len(searchResults) - 1
	} else {
		searchIndex--
	}
	highlightCurrentMatch()
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
		if event.Key() == tcell.KeyF2 {
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
		if event.Key() == tcell.KeyCtrlW {
			// INFO: continue bot/text message
			// without new role
			lastRole := chatBody.Messages[len(chatBody.Messages)-1].Role
			// go chatRound("", lastRole, textView, false, true)
			chatRoundChan <- &models.ChatRoundReq{Role: lastRole, Resume: true}
			return nil
		}
		if event.Key() == tcell.KeyCtrlQ {
			// Show user role selection popup instead of cycling through roles
			showUserRoleSelectionPopup()
			return nil
		}
		if event.Key() == tcell.KeyCtrlX {
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
				// Also clear any image attachment after sending the message
				go func() {
					// Wait a short moment for the message to be processed, then clear the image attachment
					// This allows the image to be sent with the current message if it was attached
					// But clears it for the next message
					ClearImageAttachment()
				}()
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
