package main

import (
	"errors"
	"fmt"
	"gf-lt/models"
	"gf-lt/pngmeta"
	"gf-lt/tools"
	"image"
	"math/rand/v2"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/nfnt/resize"
	"github.com/rivo/tview"
	"gitlab.com/diamondburned/ueberzug-go"
)

// Cached model color - updated by background goroutine
// var cachedModelColor string = "orange"
var cachedModelColor atomic.Value

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
		cachedModelColor.Store("orange")
		return
	}
	// Check if model is loaded
	loaded, err := isModelLoaded(chatBody.Model)
	if err != nil {
		// On error, assume not loaded (red)
		cachedModelColor.Store("red")
		return
	}
	if loaded {
		cachedModelColor.Store("green")
	} else {
		cachedModelColor.Store("red")
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

func mapToString[V any](m map[string]V) string {
	rs := strings.Builder{}
	for k, v := range m {
		fmt.Fprintf(&rs, "%v: %v\n", k, v)
	}
	return rs.String()
}

// stripThinkingFromMsg removes thinking blocks from assistant messages.
// Skips user, tool, and system messages as they may contain thinking examples.
func stripThinkingFromMsg(msg *models.RoleMsg) *models.RoleMsg {
	if !cfg.StripThinkingFromAPI {
		return msg
	}
	// Skip user, tool, they might contain thinking and system messages - examples
	if msg.Role == cfg.UserRole || msg.Role == cfg.ToolRole || msg.Role == "system" {
		return msg
	}
	// Strip thinking from assistant messages
	msgText := msg.GetText()
	if models.ThinkRE.MatchString(msgText) {
		cleanedText := models.ThinkRE.ReplaceAllString(msgText, "")
		cleanedText = strings.TrimSpace(cleanedText)
		msg.SetText(cleanedText)
	}
	return msg
}

// refreshChatDisplay updates the chat display based on current character view
// It filters messages for the character the user is currently "writing as"
// and updates the textView with the filtered conversation
func refreshChatDisplay() {
	if cfg.CLIMode {
		return
	}
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
	if cfg.AutoScrollEnabled {
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
	text = models.CodeBlockRE.ReplaceAllStringFunc(text, func(match string) string {
		// Style the code block and store it
		styled := fmt.Sprintf("[red::i]%s[-:-:-]", match)
		codeBlocks = append(codeBlocks, styled)
		// Generate a unique placeholder (e.g., "__CODE_BLOCK_0__")
		id := fmt.Sprintf(placeholder, counter)
		counter++
		return id
	})
	text = models.ThinkRE.ReplaceAllStringFunc(text, func(match string) string {
		// Style the code block and store it
		styled := fmt.Sprintf("[red::i]%s[-:-:-]", match)
		thinkBlocks = append(thinkBlocks, styled)
		// Generate a unique placeholder (e.g., "__CODE_BLOCK_0__")
		id := fmt.Sprintf(placeholderThink, counterThink)
		counterThink++
		return id
	})
	// Step 2: Apply other regex styles to the non-code parts
	text = models.QuotesRE.ReplaceAllString(text, `[orange::-]$1[-:-:-]`)
	text = models.StarRE.ReplaceAllString(text, `[turquoise::i]$1[-:-:-]`)
	text = models.SingleBacktickRE.ReplaceAllString(text, "`[pink::i]$1[-:-:-]`")
	// text = tools.ThinkRE.ReplaceAllString(text, `[yellow::i]$1[-:-:-]`)
	// Step 3: Restore the styled code blocks from placeholders
	for i, cb := range codeBlocks {
		text = strings.Replace(text, fmt.Sprintf(placeholder, i), cb, 1)
	}
	for i, tb := range thinkBlocks {
		text = strings.Replace(text, fmt.Sprintf(placeholderThink, i), tb, 1)
	}
	// text = strings.ReplaceAll(text, `$\rightarrow$`, "->")
	text = RenderLatex(text)
	text = alignMarkdownTables(text)
	textView.SetText(text)
}

func updateStatusLine() {
	if cfg.CLIMode {
		return // no status line in cli mode
	}
	if statusLineWidget == nil {
		return // TUI not initialized yet
	}
	status := makeStatusLine()
	statusLineWidget.SetText(status)
	updateImageOverlay()
}

func initSysCards() ([]*models.CharCard, error) {
	cards := []*models.CharCard{}
	// Always include the basic (default) assistant card
	assistantCard := &models.CharCard{
		ID:        basicCard.ID,
		SysPrompt: basicCard.SysPrompt,
		FirstMsg:  basicCard.FirstMsg,
		Role:      basicCard.Role,
		FilePath:  basicCard.FilePath,
	}
	cards = append(cards, assistantCard)
	fileCards, err := pngmeta.ReadDirCards(cfg.SysDir, cfg.UserRole, logger)
	if err != nil {
		logger.Error("failed to read sys dir", "error", err)
		return nil, err
	}
	for _, cc := range fileCards {
		if cc.Role == "" {
			logger.Warn("empty role", "file", cc.FilePath)
			continue
		}
		if cc.ID == "" {
			cc.ID = models.ComputeCardID(cc.Role, cc.FilePath)
		}
		sysMap[cc.ID] = cc
		roleToID[cc.Role] = cc.ID
		cards = append(cards, cc)
	}
	return cards, nil
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
	cardID := currentCardID
	if cardID == "" {
		cardID = roleToID[cfg.AssistantRole]
	}
	newChat := &models.Chat{
		ID:        id + 1,
		Name:      fmt.Sprintf("%d_%s", id+1, cfg.AssistantRole),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		// chat is written to db when we get first llm response (or any)
		// actual chat history (messages) would be parsed then
		Msgs:  "",
		Agent: cardID,
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

func loadImage() error {
	filepath := defaultImage
	cc := GetCardByRole(cfg.AssistantRole)
	if cc != nil {
		if strings.HasSuffix(cc.FilePath, ".png") {
			filepath = cc.FilePath
		}
	}
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}
	imgView.SetImage(img)
	return nil
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
	if strings.Contains(cfg.CurrentAPI, "openrouter") || strings.Contains(cfg.CurrentAPI, "deepseek") {
		return false
	}
	return true
}

// getModelColor returns the cached color tag for the model name.
// The cached value is updated by a background goroutine every 5 seconds.
// For non-local models, returns orange. For local llama.cpp models, returns green if loaded, red if not.
func getModelColor() string {
	return cachedModelColor.Load().(string)
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
	if len(pendingImageAttachments) > 0 {
		if len(pendingImageAttachments) == 1 {
			imageName := path.Base(pendingImageAttachments[0])
			imageInfo = fmt.Sprintf(" | attached img: [orange:-:b]%s[-:-:-]", imageName)
		} else {
			names := make([]string, len(pendingImageAttachments))
			for i, p := range pendingImageAttachments {
				names[i] = path.Base(p)
			}
			last := names[len(names)-1]
			count := len(names) - 1
			if count == 1 {
				imageInfo = fmt.Sprintf(" | attached imgs: [orange:-:b]%s[-:-:-] +1 more", last)
			} else {
				imageInfo = fmt.Sprintf(" | attached imgs: [orange:-:b]%s[-:-:-] +%d more", last, count)
			}
		}
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
	statusLine := fmt.Sprintf(statusLineTempl, activeChatName,
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
	// context tokens
	contextTokens := getContextTokens()
	maxCtx := getMaxContextTokens()
	if maxCtx == 0 {
		maxCtx = 16384
	}
	if contextTokens > 0 {
		contextInfo := fmt.Sprintf(" | context-estim: [orange:-:b]%d/%d[-:-:-]", contextTokens, maxCtx)
		statusLine += contextInfo
	}
	row, _ := textView.GetScrollOffset()
	scrollInfo := fmt.Sprintf(" | [green:-:b]row=%d[-:-:-]", row)
	if first, last, atTop, atBottom, ok := getVisibleMsgRange(); ok {
		var label string
		if first == last {
			label = fmt.Sprintf("msg=%d", first)
		} else {
			label = fmt.Sprintf("msgs=%d-%d", first, last)
		}
		switch {
		case atTop && atBottom:
			label += " ALL"
		case atTop:
			label += " TOP"
		case atBottom:
			label += " END"
		}
		scrollInfo += fmt.Sprintf(" [green:-:b]%s[-:-:-]", label)
	}
	statusLine += scrollInfo
	return statusLine + imageInfo + shellModeInfo
}

// msgLineRange tracks which rendered line range a message occupies.
type msgLineRange struct {
	msgIdx    int
	lineStart int
	lineEnd   int
}

// parseMsgHeaderIdx extracts the message index from a line if it starts
// with a message header like "(0) <role>:"
func parseMsgHeaderIdx(line string) int {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "(") {
		return -1
	}
	closeParen := strings.IndexByte(line, ')')
	if closeParen < 0 {
		return -1
	}
	idx, err := strconv.Atoi(line[1:closeParen])
	if err != nil {
		return -1
	}
	rest := strings.TrimSpace(line[closeParen+1:])
	if !strings.HasPrefix(rest, "<") {
		return -1
	}
	gtEnd := strings.IndexByte(rest, '>')
	if gtEnd < 0 {
		return -1
	}
	if strings.TrimSpace(rest[gtEnd+1:]) != ":" {
		return -1
	}
	return idx
}

// countRenderedLinesOfLine returns how many rendered (wrapped) lines a
// single logical line occupies at the given terminal width.
func countRenderedLinesOfLine(line string, width int) int {
	if width <= 0 {
		width = 80
	}
	lineWidth := tview.TaggedStringWidth(line)
	if lineWidth <= 0 {
		return 1
	}
	rendered := (lineWidth + width - 1) / width
	return max(rendered, 1)
}

// getVisibleMsgRange returns the range of message indices currently visible
// in the textView widget, plus whether the view is at the top or bottom
// of the text. Returns ok=false if no messages are visible or the chat
// is empty.
func getVisibleMsgRange() (first, last int, atTop, atBottom bool, ok bool) {
	row, _ := textView.GetScrollOffset()
	_, _, width, height := textView.GetInnerRect()
	if width <= 0 || height <= 0 {
		return 0, 0, false, false, false
	}
	lastVisible := row + height - 1

	text := textView.GetText(true)
	if text == "" {
		return 0, 0, false, false, false
	}

	logicalLines := strings.Split(text, "\n")

	var (
		msgSpans  []msgLineRange
		cursor    int
		curIdx    = -1
		spanStart int
	)

	for _, logicalLine := range logicalLines {
		renderedLines := countRenderedLinesOfLine(logicalLine, width)
		renderedEnd := cursor + renderedLines

		if idx := parseMsgHeaderIdx(logicalLine); idx >= 0 {
			if curIdx >= 0 {
				msgSpans = append(msgSpans, msgLineRange{
					msgIdx:    curIdx,
					lineStart: spanStart,
					lineEnd:   cursor,
				})
			}
			curIdx = idx
			spanStart = cursor
		}

		cursor = renderedEnd
	}

	if curIdx >= 0 {
		msgSpans = append(msgSpans, msgLineRange{
			msgIdx:    curIdx,
			lineStart: spanStart,
			lineEnd:   cursor,
		})
	}

	first = -1
	last = -1
	for _, s := range msgSpans {
		if s.lineStart <= lastVisible && s.lineEnd > row {
			if first == -1 || s.msgIdx < first {
				first = s.msgIdx
			}
			if s.msgIdx > last {
				last = s.msgIdx
			}
		}
	}

	if first < 0 {
		return 0, 0, false, false, false
	}
	atTop = row == 0
	atBottom = row+height >= cursor
	return first, last, atTop, atBottom, true
}

// findFirstImageInMsg returns the file path of the first valid image in the
// given message, or empty string if none found.
func findFirstImageInMsg(msgIdx int) string {
	if msgIdx < 0 || msgIdx >= len(chatBody.Messages) {
		return ""
	}
	msg := &chatBody.Messages[msgIdx]
	if !msg.HasContentParts {
		return ""
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
			}
		}
		if displayPath != "" && tools.IsImageFile(displayPath) {
			if _, err := os.Stat(displayPath); err == nil {
				return displayPath
			}
		}
	}
	return ""
}

// destroyChatOverlay destroys the current ueberzug image overlay if any.
func destroyChatOverlay() {
	if currentChatOverlayImg != nil {
		currentChatOverlayImg.Destroy()
		currentChatOverlayImg = nil
	}
}

// displayImageOverlay decodes, resizes, and shows an image overlay at the
// top-right corner of the terminal using ueberzug.
func displayImageOverlay(imgPath string) {
	file, err := os.Open(imgPath)
	if err != nil {
		destroyChatOverlay()
		return
	}
	defer file.Close()

	imgData, _, err := image.Decode(file)
	if err != nil {
		destroyChatOverlay()
		return
	}

	const maxSize = 500
	scaledImg := resize.Resize(0, uint(maxSize), imgData, resize.Lanczos3)

	geom, err := getTerminalGeometry()
	if err != nil {
		destroyChatOverlay()
		return
	}

	bounds := scaledImg.Bounds()
	cellH := geom.Height / geom.Rows
	padding := cellH
	pixelX := geom.X + geom.Width - bounds.Dx() - padding
	pixelY := geom.Y + padding

	destroyChatOverlay()

	uimg, err := ueberzug.NewImage(scaledImg, pixelX, pixelY)
	if err != nil {
		return
	}
	currentChatOverlayImg = uimg
}

// updateImageOverlay checks the currently visible message range and shows an
// image overlay for the best candidate (middle-most visible message with an
// image). It skips if nothing changed since the last call.
func updateImageOverlay() {
	if !ueberzugAvailable {
		return
	}

	currentRow, _ := textView.GetScrollOffset()
	if currentRow == overlayLastRow {
		return
	}

	first, last, _, _, ok := getVisibleMsgRange()
	if !ok {
		destroyChatOverlay()
		overlayLastRow = currentRow
		overlayLastMsgIdx = -1
		return
	}

	midMsg := (first + last) / 2
	bestIdx := -1
	bestPath := ""

	for offset := 0; offset <= max(midMsg-first, last-midMsg); offset++ {
		for _, tryIdx := range []int{midMsg + offset, midMsg - offset} {
			if tryIdx < first || tryIdx > last {
				continue
			}
			if tryIdx == overlayLastMsgIdx {
				continue
			}
			if path := findFirstImageInMsg(tryIdx); path != "" {
				if bestIdx == -1 {
					bestIdx = tryIdx
					bestPath = path
				}
				if tryIdx == midMsg {
					goto found
				}
			}
		}
	}

	// Fall back to last visible message
	if bestIdx == -1 {
		if path := findFirstImageInMsg(last); path != "" {
			bestIdx = last
			bestPath = path
		}
	}

found:
	if bestIdx < 0 || bestPath == "" {
		destroyChatOverlay()
		overlayLastRow = currentRow
		overlayLastMsgIdx = -1
		return
	}

	displayImageOverlay(bestPath)
	overlayLastRow = currentRow
	overlayLastMsgIdx = bestIdx
}

func getContextTokens() int {
	if chatBody == nil || chatBody.Messages == nil {
		return 0
	}
	total := 0
	messages := chatBody.Messages
	for i := range messages {
		msg := &messages[i]
		if msg.Stats != nil && msg.Stats.Tokens > 0 {
			total += msg.Stats.Tokens
		} else if msg.GetText() != "" {
			total += len(msg.GetText()) / 4
		}
	}
	return total
}

const deepseekContext = 128000

func getMaxContextTokens() int {
	if chatBody == nil || chatBody.Model == "" {
		return 0
	}
	modelName := chatBody.Model
	switch {
	case strings.Contains(cfg.CurrentAPI, "openrouter"):
		if orModelsData != nil {
			for i := range orModelsData.Data {
				m := &orModelsData.Data[i]
				if m.ID == modelName {
					return m.ContextLength
				}
			}
		}
	case strings.Contains(cfg.CurrentAPI, "deepseek"):
		return deepseekContext
	default:
		if localModelsData != nil {
			for i := range localModelsData.Data {
				m := &localModelsData.Data[i]
				if m.ID == modelName {
					for _, arg := range m.Status.Args {
						if strings.HasPrefix(arg, "--ctx-size") {
							if strings.Contains(arg, "=") {
								val := strings.Split(arg, "=")[1]
								if n, err := strconv.Atoi(val); err == nil {
									return n
								}
							} else {
								idx := -1
								for j, a := range m.Status.Args {
									if a == "--ctx-size" && j+1 < len(m.Status.Args) {
										idx = j + 1
										break
									}
								}
								if idx != -1 {
									if n, err := strconv.Atoi(m.Status.Args[idx]); err == nil {
										return n
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return 0
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
		logger.Warn("failed to find current card", "agent", currentChat.Agent)
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
			showToast("bad request", "wrong deepseek model name")
			return nil
		}
	}
	return nil
}

// == shellmode ==

func toggleShellMode() {
	shellMode = !shellMode
	setShellMode(shellMode)
	if shellMode {
		shellInput.SetLabel(fmt.Sprintf("[%s]$ ", cfg.FilePickerDir))
	} else {
		textArea.SetPlaceholder("input is multiline; press <Enter> to start the next line;\npress <Esc> to send the message.")
	}
	updateStatusLine()
}

func updateFlexLayout() {
	if fullscreenMode {
		// flex already contains only focused widget; do nothing
		return
	}
	destroyChatOverlay()
	flex.Clear()
	flex.AddItem(textView, 0, 40, false)
	if shellMode {
		flex.AddItem(shellInput, 0, 10, false)
	} else {
		flex.AddItem(bottomFlex, 0, 10, true)
	}
	if positionVisible {
		flex.AddItem(statusLineWidget, 0, 2, false)
	}
	// Keep focus on currently focused widget
	focused := app.GetFocus()
	switch {
	case focused == textView:
		app.SetFocus(textView)
	case shellMode:
		app.SetFocus(shellInput)
	default:
		app.SetFocus(textArea)
	}
}

func executeCommandAndDisplay(cmdText string) {
	cmdText = strings.TrimSpace(cmdText)
	if cmdText == "" {
		fmt.Fprintf(textView, "\n[red]Error: No command provided[-:-:-]\n")
		if cfg.AutoScrollEnabled {
			textView.ScrollToEnd()
		}
		colorText()
		return
	}
	workingDir := cfg.FilePickerDir
	// Handle cd command specially to update working directory
	if strings.HasPrefix(cmdText, "cd ") {
		newDir := strings.TrimPrefix(cmdText, "cd ")
		newDir = strings.TrimSpace(newDir)
		// Handle cd ~ or cdHOME
		if strings.HasPrefix(newDir, "~") {
			home := os.Getenv("HOME")
			newDir = strings.Replace(newDir, "~", home, 1)
		}
		// Check if directory exists
		if _, err := os.Stat(newDir); err == nil {
			workingDir = newDir
			cfg.FilePickerDir = workingDir
			// Update shell input label with new directory
			shellInput.SetLabel(fmt.Sprintf("[%s]$ ", cfg.FilePickerDir))
			outputContent := workingDir
			// Add the command being executed to the chat
			fmt.Fprintf(textView, "\n[-:-:b](%d) <%s>: [-:-:-]\n$ %s\n",
				len(chatBody.Messages), cfg.ToolRole, cmdText)
			fmt.Fprintf(textView, "%s\n", outputContent)
			combinedMsg := models.RoleMsg{
				Role:    cfg.ToolRole,
				Content: "$ " + cmdText + "\n\n" + outputContent,
			}
			chatBody.Messages = append(chatBody.Messages, combinedMsg)
			if cfg.AutoScrollEnabled {
				textView.ScrollToEnd()
			}
			colorText()
			return
		} else {
			outputContent := "cd: " + newDir + ": No such file or directory"
			fmt.Fprintf(textView, "\n[-:-:b](%d) <%s>: [-:-:-]\n$ %s\n",
				len(chatBody.Messages), cfg.ToolRole, cmdText)
			fmt.Fprintf(textView, "[red]%s[-:-:-]\n", outputContent)
			combinedMsg := models.RoleMsg{
				Role:    cfg.ToolRole,
				Content: "$ " + cmdText + "\n\n" + outputContent,
			}
			chatBody.Messages = append(chatBody.Messages, combinedMsg)
			if cfg.AutoScrollEnabled {
				textView.ScrollToEnd()
			}
			colorText()
			return
		}
	}
	// Use /bin/sh to support pipes, redirects, etc.
	cmd := exec.Command("/bin/sh", "-c", cmdText)
	cmd.Dir = workingDir
	// Execute the command and get output
	output, err := cmd.CombinedOutput()
	// Add the command being executed to the chat
	fmt.Fprintf(textView, "\n[-:-:b](%d) <%s>: [-:-:-]\n$ %s\n",
		len(chatBody.Messages), cfg.ToolRole, cmdText)
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
	if cfg.AutoScrollEnabled {
		textView.ScrollToEnd()
	}
	colorText()
	// Add command to history (avoid duplicates at the end)
	if len(shellHistory) == 0 || shellHistory[len(shellHistory)-1] != cmdText {
		shellHistory = append(shellHistory, cmdText)
	}
	shellHistoryPos = -1
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
		showToast("search", notification)
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
	showToast("search", notification)
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
		showToast("search", "No search results to navigate")
		return
	}
	searchIndex = (searchIndex + 1) % len(searchResults)
	highlightCurrentMatch()
}

// searchPrev finds the previous occurrence of the search term
func searchPrev() {
	if len(searchResults) == 0 {
		showToast("search", "No search results to navigate")
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

// models logic that is too complex for models package
func MsgToText(i int, m *models.RoleMsg) string {
	var contentStr string
	var imageIndicators []string
	if !m.HasContentParts {
		contentStr = m.Content
	} else {
		var textParts []string
		for _, part := range m.ContentParts {
			switch p := part.(type) {
			case models.TextContentPart:
				if p.Type == "text" {
					textParts = append(textParts, p.Text)
				}
			case models.ImageContentPart:
				displayPath := p.Path
				if displayPath == "" {
					displayPath = "image"
				} else {
					displayPath = extractDisplayPath(displayPath, cfg.FilePickerDir)
				}
				imageIndicators = append(imageIndicators, fmt.Sprintf("[orange::i][image: %s][-:-:-]", displayPath))
			case map[string]any:
				if partType, exists := p["type"]; exists {
					switch partType {
					case "text":
						if textVal, textExists := p["text"]; textExists {
							if textStr, isStr := textVal.(string); isStr {
								textParts = append(textParts, textStr)
							}
						}
					case "image_url":
						var displayPath string
						if pathVal, pathExists := p["path"]; pathExists {
							if pathStr, isStr := pathVal.(string); isStr && pathStr != "" {
								displayPath = extractDisplayPath(pathStr, cfg.FilePickerDir)
							}
						}
						if displayPath == "" {
							displayPath = "image"
						}
						imageIndicators = append(imageIndicators, fmt.Sprintf("[orange::i][image: %s][-:-:-]", displayPath))
					}
				}
			}
		}
		contentStr = strings.Join(textParts, " ") + " "
	}
	contentStr, _ = strings.CutPrefix(contentStr, m.Role+":")
	icon := fmt.Sprintf("(%d) <%s>: ", i, m.Role)
	var finalContent strings.Builder
	if len(imageIndicators) > 0 {
		for _, indicator := range imageIndicators {
			finalContent.WriteString(indicator)
			finalContent.WriteString("\n")
		}
	}
	finalContent.WriteString(contentStr)
	if m.Stats != nil {
		fmt.Fprintf(&finalContent, "\n[gray::i][%d tok, %.1fs, %.1f t/s][-:-:-]", m.Stats.Tokens, m.Stats.Duration, m.Stats.TokensPerSec)
	}
	textMsg := fmt.Sprintf("[-:-:b]%s[-:-:-]\n%s\n", icon, finalContent.String())
	return strings.ReplaceAll(textMsg, "\n\n", "\n")
}

// extractDisplayPath returns a path suitable for display, potentially relative to imageBaseDir
func extractDisplayPath(p, bp string) string {
	if p == "" {
		return ""
	}
	// If base directory is set, try to make path relative to it
	if bp != "" {
		if rel, err := filepath.Rel(bp, p); err == nil {
			// Check if relative path doesn't start with ".." (meaning it's within base dir)
			// If it starts with "..", we might still want to show it as relative
			// but for now we show full path if it goes outside base dir
			if !strings.HasPrefix(rel, "..") {
				p = rel
			}
		}
	}
	// Truncate long paths to last 60 characters if needed
	if len(p) > 60 {
		return "..." + p[len(p)-60:]
	}
	return p
}

func getValidKnowToRecipient(msg *models.RoleMsg) (string, bool) {
	if cfg == nil || !cfg.CharSpecificContextEnabled {
		return "", false
	}
	// case where all roles are in the tag => public message
	cr := listChatRoles()
	slices.Sort(cr)
	slices.Sort(msg.KnownTo)
	if slices.Equal(cr, msg.KnownTo) {
		logger.Info("got msg with tag mentioning every role")
		return "", false
	}
	// Check each character in the KnownTo list
	for _, recipient := range msg.KnownTo {
		if recipient == msg.Role || recipient == cfg.ToolRole {
			// weird cases, skip
			continue
		}
		// Skip if this is the user character (user handles their own turn)
		// If user is in KnownTo, stop processing - it's the user's turn
		if recipient == cfg.UserRole || recipient == cfg.WriteNextMsgAs {
			return "", false
		}
		return recipient, true
	}
	return "", false
}

// triggerPrivateMessageResponses checks if a message was sent privately to specific characters
// and triggers those non-user characters to respond
func triggerPrivateMessageResponses(msg *models.RoleMsg) {
	recipient, ok := getValidKnowToRecipient(msg)
	if !ok || recipient == "" {
		return
	}
	// Trigger the recipient character to respond
	triggerMsg := recipient + ":\n"
	// Send empty message so LLM continues naturally from the conversation
	crr := &models.ChatRoundReq{
		UserMsg: triggerMsg,
		Role:    recipient,
		Resume:  true,
	}
	fmt.Fprintf(textView, "\n[-:-:b](%d) ", len(chatBody.Messages))
	fmt.Fprint(textView, roleToIcon(recipient))
	fmt.Fprint(textView, "[-:-:-]\n")
	chatRoundChan <- crr
}

func GetCardByRole(role string) *models.CharCard {
	cardID, ok := roleToID[role]
	if !ok {
		return nil
	}
	return sysMap[cardID]
}

func notifySend(topic, message string) error {
	// Sanitize message to remove control characters that notify-send doesn't handle
	sanitized := strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' {
			return -1
		}
		return r
	}, message)
	// Truncate if too long
	if len(sanitized) > 200 {
		sanitized = sanitized[:197] + "..."
	}
	cmd := exec.Command("notify-send", topic, sanitized)
	return cmd.Run()
}

type TermGeometry struct {
	X, Y, Width, Height int
	Cols, Rows          int
}

func getTerminalGeometry() (TermGeometry, error) {
	var geom TermGeometry
	out, err := exec.Command("xdotool", "getactivewindow").Output()
	if err != nil {
		return TermGeometry{}, err
	}
	winID := strings.TrimSpace(string(out))
	if winID == "0" {
		return TermGeometry{}, errors.New("no active window")
	}
	out, err = exec.Command("xdotool", "getwindowgeometry", winID).Output()
	if err != nil {
		return TermGeometry{}, err
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Position:") {
			_, _ = fmt.Sscanf(line, " Position: %d,%d", &geom.X, &geom.Y)
		} else if strings.Contains(line, "Geometry:") {
			_, _ = fmt.Sscanf(line, " Geometry: %dx%d", &geom.Width, &geom.Height)
		}
	}
	if geom.Width == 0 || geom.Height == 0 {
		return TermGeometry{}, errors.New("invalid window geometry")
	}
	// Use terminal size captured at init time
	if termCols > 0 && termRows > 0 {
		geom.Cols = termCols
		geom.Rows = termRows
	}
	// Fallback to env vars
	if geom.Cols == 0 || geom.Rows == 0 {
		if cols := os.Getenv("COLUMNS"); cols != "" {
			if n, err := strconv.Atoi(cols); err == nil && n > 0 {
				geom.Cols = n
			}
		}
		if rows := os.Getenv("LINES"); rows != "" {
			if n, err := strconv.Atoi(rows); err == nil && n > 0 {
				geom.Rows = n
			}
		}
	}
	// Fallback to stty
	if geom.Cols == 0 || geom.Rows == 0 {
		out, err := exec.Command("stty", "size").Output()
		logger.Warn("stty result", "error", err, "output", string(out))
		if err == nil {
			trimmed := strings.TrimSpace(string(out))
			if trimmed != "" {
				_, _ = fmt.Sscanf(trimmed, "%d %d", &geom.Rows, &geom.Cols)
			}
		}
	}
	if geom.Cols == 0 || geom.Rows == 0 {
		logger.Warn("terminal geometry check", "cols", geom.Cols, "rows", geom.Rows, "termCols", termCols, "termRows", termRows)
		return TermGeometry{}, errors.New("invalid terminal size")
	}
	return geom, nil
}

func cellToPixel(cellX, cellY int, geom TermGeometry) (pixelX, pixelY int) {
	cellWidth := geom.Width / geom.Cols
	cellHeight := geom.Height / geom.Rows
	return geom.X + cellX*cellWidth, geom.Y + cellY*cellHeight
}

func rollReqToRollResult(rr string) string {
	res := strings.Split(rr, " ")
	if len(res) < 2 {
		return ""
	}
	maxNum, err := strconv.ParseInt(res[1], 10, 64)
	if err != nil {
		return ""
	}
	roll := rand.Int64N(maxNum) + 1
	return fmt.Sprintf("roll of %d; result is %d", maxNum, roll)
}
