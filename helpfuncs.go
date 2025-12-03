package main

import (
	"fmt"
	"gf-lt/models"
	"gf-lt/pngmeta"
	"image"
	"os"
	"path"
	"strings"
)

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
	position.SetText(makeStatusLine())
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

func renameUser(oldname, newname string) {
	if oldname == "" {
		// not provided; deduce who user is
		// INFO: if user not yet spoke, it is hard to replace mentions in sysprompt and first message about thme
		roles := chatBody.ListRoles()
		for _, role := range roles {
			if role == cfg.AssistantRole {
				continue
			}
			if role == cfg.ToolRole {
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
	roles := chatBody.ListRoles()
	// Remove user role if it exists in the list (to avoid duplicates and ensure it's at position 0)
	filteredRoles := make([]string, 0, len(roles))
	for _, role := range roles {
		if role != cfg.UserRole {
			filteredRoles = append(filteredRoles, role)
		}
	}
	// Prepend user role to the beginning of the list
	result := append([]string{cfg.UserRole}, filteredRoles...)
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

	statusLine := fmt.Sprintf(indexLineCompletion, botRespMode, cfg.AssistantRole, activeChatName,
		cfg.ToolUse, chatBody.Model, cfg.SkipLLMResp, cfg.CurrentAPI, cfg.ThinkUse, logLevel.Level(),
		isRecording, persona, botPersona, injectRole)
	return statusLine + imageInfo + shellModeInfo
}
