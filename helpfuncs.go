package main

import (
	"fmt"
	"gf-lt/models"
	"gf-lt/pngmeta"
	"image"
	"os"
	"path"
	"slices"
	"strings"
	"unicode"

	"math/rand/v2"
)

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
	if scrollToEndEnabled {
		textView.ScrollToEnd()
	}
}

func stopTTSIfNotForUser(msg *models.RoleMsg) {
	viewingAs := cfg.UserRole
	if cfg.WriteNextMsgAs != "" {
		viewingAs = cfg.WriteNextMsgAs
	}
	// stop tts if msg is not for user
	if cfg.CharSpecificContextEnabled &&
		!slices.Contains(msg.KnownTo, viewingAs) && cfg.TTS_ENABLED {
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
	statusLineWidget.SetText(makeStatusLine())
	helpView.SetText(fmt.Sprintf(helpText, makeStatusLine()))
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
	statusLine := fmt.Sprintf(indexLineCompletion, boolColors[botRespMode], botRespMode, activeChatName,
		boolColors[cfg.ToolUse], cfg.ToolUse, chatBody.Model, boolColors[cfg.SkipLLMResp],
		cfg.SkipLLMResp, cfg.CurrentAPI, boolColors[isRecording], isRecording, persona,
		botPersona, boolColors[injectRole], injectRole)
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
