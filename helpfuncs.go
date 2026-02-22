package main

import (
	"fmt"
	"gf-lt/models"
	"gf-lt/pngmeta"
	"image"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"math/rand/v2"

	"github.com/rivo/tview"
)

// Cached model color - updated by background goroutine
var cachedModelColor string = "orange"

// startModelColorUpdater starts a background goroutine that periodically updates
// the cached model color. Only runs HTTP requests for local llama.cpp APIs.
func startModelColorUpdater() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		// Initial check
		updateCachedModelColor()

		for range ticker.C {
			updateCachedModelColor()
		}
	}()
}

// updateCachedModelColor updates the global cachedModelColor variable
func updateCachedModelColor() {
	if !isLocalLlamacpp() {
		cachedModelColor = "orange"
		return
	}

	// Check if model is loaded
	loaded, err := isModelLoaded(chatBody.Model)
	if err != nil {
		// On error, assume not loaded (red)
		cachedModelColor = "red"
		return
	}
	if loaded {
		cachedModelColor = "green"
	} else {
		cachedModelColor = "red"
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// stripThinkingFromMsg removes thinking blocks from assistant messages.
// Skips user, tool, and system messages as they may contain thinking examples.
func stripThinkingFromMsg(msg *models.RoleMsg) *models.RoleMsg {
	if !cfg.StripThinkingFromAPI {
		return msg
	}
	// Skip user, tool, and system messages - they might contain thinking examples
	if msg.Role == cfg.UserRole || msg.Role == cfg.ToolRole || msg.Role == "system" {
		return msg
	}
	// Strip thinking from assistant messages
	if thinkRE.MatchString(msg.Content) {
		msg.Content = thinkRE.ReplaceAllString(msg.Content, "")
		// Clean up any double newlines that might result
		msg.Content = strings.TrimSpace(msg.Content)
	}
	return msg
}

// refreshChatDisplay updates the chat display based on current character view
// It filters messages for the character the user is currently "writing as"
// and updates the textView with the filtered conversation
func refreshChatDisplay() {
	// Determine which character's view to show
	viewingAs := cfg.UserRole
	if cfg.WriteNextMsgAs != "" {
		viewingAs = cfg.WriteNextMsgAs
	}
	// Filter messages for this character
	filteredMessages := filterMessagesForCharacter(chatBody.Messages, viewingAs)
	displayText := chatToText(filteredMessages, cfg.ShowSys)
	textView.SetText(displayText)
	colorText()
	updateStatusLine()
	if scrollToEndEnabled {
		textView.ScrollToEnd()
	}
}

// stopTTSIfNotForUser: character specific context, not meant fot the human to hear
func stopTTSIfNotForUser(msg *models.RoleMsg) {
	if strings.Contains(cfg.CurrentAPI, "/chat") || !cfg.CharSpecificContextEnabled {
		return
	}
	viewingAs := cfg.UserRole
	if cfg.WriteNextMsgAs != "" {
		viewingAs = cfg.WriteNextMsgAs
	}
	// stop tts if msg is not for user
	if !slices.Contains(msg.KnownTo, viewingAs) && cfg.TTS_ENABLED {
		TTSDoneChan <- true
	}
}

func colorText() {
	text := textView.GetText(false)
	quoteReplacer := strings.NewReplacer(
		`”`, `"`,
		`“`, `"`,
		`“`, `"`,
		`”`, `"`,
		`**`, `*`,
	)
	text = quoteReplacer.Replace(text)
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
	text = singleBacktickRE.ReplaceAllString(text, "`[pink::i]$1[-:-:-]`")
	// text = thinkRE.ReplaceAllString(text, `[yellow::i]$1[-:-:-]`)
	// Step 3: Restore the styled code blocks from placeholders
	for i, cb := range codeBlocks {
		text = strings.Replace(text, fmt.Sprintf(placeholder, i), cb, 1)
	}
	for i, tb := range thinkBlocks {
		text = strings.Replace(text, fmt.Sprintf(placeholderThink, i), tb, 1)
	}
	textView.SetText(text)
}

func updateStatusLine() {
	status := makeStatusLine()
	statusLineWidget.SetText(status)
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

func startNewChat(keepSysP bool) {
	id, err := store.ChatGetMaxID()
	if err != nil {
		logger.Error("failed to get chat id", "error", err)
	}
	if ok := charToStart(cfg.AssistantRole, keepSysP); !ok {
		logger.Warn("no such sys msg", "name", cfg.AssistantRole)
	}
	// set chat body
	chatBody.Messages = chatBody.Messages[:2]
	textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
	newChat := &models.Chat{
		ID:   id + 1,
		Name: fmt.Sprintf("%d_%s", id+1, cfg.AssistantRole),
		// chat is written to db when we get first llm response (or any)
		// actual chat history (messages) would be parsed then
		Msgs:  "",
		Agent: cfg.AssistantRole,
	}
	activeChatName = newChat.Name
	chatMap[newChat.Name] = newChat
	updateStatusLine()
	colorText()
}

func renameUser(oldname, newname string) {
	if oldname == "" {
		// not provided; deduce who user is
		// INFO: if user not yet spoke, it is hard to replace mentions in sysprompt and first message about thme
		roles := chatBody.ListRoles()
		for _, role := range roles {
			if role == cfg.AssistantRole {
				continue
			}
			if role == "tool" {
				continue
			}
			if role == "system" {
				continue
			}
			oldname = role
			break
		}
		if oldname == "" {
			// still
			logger.Warn("fn: renameUser; failed to find old name", "newname", newname)
			return
		}
	}
	viewText := textView.GetText(false)
	viewText = strings.ReplaceAll(viewText, oldname, newname)
	chatBody.Rename(oldname, newname)
	textView.SetText(viewText)
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

func listRolesWithUser() []string {
	roles := listChatRoles()
	// Remove user role if it exists in the list (to avoid duplicates and ensure it's at position 0)
	filteredRoles := make([]string, 0, len(roles))
	for _, role := range roles {
		if role != cfg.UserRole {
			filteredRoles = append(filteredRoles, role)
		}
	}
	// Prepend user role to the beginning of the list
	result := append([]string{cfg.UserRole}, filteredRoles...)
	slices.Sort(result)
	return result
}

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

func strInSlice(s string, sl []string) bool {
	for _, el := range sl {
		if strings.EqualFold(s, el) {
			return true
		}
	}
	return false
}

// isLocalLlamacpp checks if the current API is a local llama.cpp instance.
func isLocalLlamacpp() bool {
	u, err := url.Parse(cfg.CurrentAPI)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// getModelColor returns the cached color tag for the model name.
// The cached value is updated by a background goroutine every 5 seconds.
// For non-local models, returns orange. For local llama.cpp models, returns green if loaded, red if not.
func getModelColor() string {
	return cachedModelColor
}

func makeStatusLine() string {
	isRecording := false
	if asr != nil {
		isRecording = asr.IsRecording()
	}
	persona := cfg.UserRole
	if cfg.WriteNextMsgAs != "" {
		persona = cfg.WriteNextMsgAs
	}
	botPersona := cfg.AssistantRole
	if cfg.WriteNextMsgAsCompletionAgent != "" {
		botPersona = cfg.WriteNextMsgAsCompletionAgent
	}
	// Add image attachment info to status line
	var imageInfo string
	if imageAttachmentPath != "" {
		// Get just the filename from the path
		imageName := path.Base(imageAttachmentPath)
		imageInfo = fmt.Sprintf(" | attached img: [orange:-:b]%s[-:-:-]", imageName)
	} else {
		imageInfo = ""
	}
	// Add shell mode status to status line
	var shellModeInfo string
	if shellMode {
		shellModeInfo = " | [green:-:b]SHELL MODE[-:-:-]"
	} else {
		shellModeInfo = ""
	}
	// Get model color based on load status for local llama.cpp models
	modelColor := getModelColor()
	statusLine := fmt.Sprintf(statusLineTempl, boolColors[botRespMode], activeChatName,
		boolColors[cfg.ToolUse], modelColor, chatBody.Model, boolColors[cfg.SkipLLMResp],
		cfg.CurrentAPI, persona, botPersona)
	if cfg.STT_ENABLED {
		recordingS := fmt.Sprintf(" | [%s:-:b]voice recording[-:-:-] (ctrl+r)",
			boolColors[isRecording])
		statusLine += recordingS
	}
	// completion endpoint
	if !strings.Contains(cfg.CurrentAPI, "chat") {
		roleInject := fmt.Sprintf(" | [%s:-:b]role injection[-:-:-] (alt+7)", boolColors[injectRole])
		statusLine += roleInject
	}
	return statusLine + imageInfo + shellModeInfo
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.IntN(len(letters))]
	}
	return string(b)
}

// set of roles within card definition and mention in chat history
func listChatRoles() []string {
	currentChat, ok := chatMap[activeChatName]
	cbc := chatBody.ListRoles()
	if !ok {
		return cbc
	}
	currentCard, ok := sysMap[currentChat.Agent]
	if !ok {
		// case which won't let to switch roles:
		// started new chat (basic_sys or any other), at the start it yet be saved or have chatbody
		// if it does not have a card or chars, it'll return an empty slice
		// log error
		logger.Warn("failed to find current card in sysMap", "agent", currentChat.Agent, "sysMap", sysMap)
		return cbc
	}
	charset := []string{}
	for _, name := range currentCard.Characters {
		if !strInSlice(name, cbc) {
			charset = append(charset, name)
		}
	}
	charset = append(charset, cbc...)
	return charset
}

func deepseekModelValidator() error {
	if cfg.CurrentAPI == cfg.DeepSeekChatAPI || cfg.CurrentAPI == cfg.DeepSeekCompletionAPI {
		if chatBody.Model != "deepseek-chat" && chatBody.Model != "deepseek-reasoner" {
			if err := notifyUser("bad request", "wrong deepseek model name"); err != nil {
				logger.Warn("failed ot notify user", "error", err)
				return err
			}
			return nil
		}
	}
	return nil
}

// == shellmode ==

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

// == search ==

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

// == tab completion ==

func scanFiles(dir, filter string) []string {
	const maxDepth = 3
	const maxFiles = 50
	var files []string
	var scanRecursive func(currentDir string, currentDepth int, relPath string)
	scanRecursive = func(currentDir string, currentDepth int, relPath string) {
		if len(files) >= maxFiles {
			return
		}
		if currentDepth > maxDepth {
			return
		}
		entries, err := os.ReadDir(currentDir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if len(files) >= maxFiles {
				return
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			fullPath := name
			if relPath != "" {
				fullPath = relPath + "/" + name
			}
			if entry.IsDir() {
				// Recursively scan subdirectories
				scanRecursive(filepath.Join(currentDir, name), currentDepth+1, fullPath)
				continue
			}
			// Check if file matches filter
			if filter == "" || strings.HasPrefix(strings.ToLower(fullPath), strings.ToLower(filter)) {
				files = append(files, fullPath)
			}
		}
	}
	scanRecursive(dir, 0, "")
	return files
}
