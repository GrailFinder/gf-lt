package main

import (
	"bufio"
	"flag"
	"fmt"
	"gf-lt/models"
	"gf-lt/pngmeta"
	"gf-lt/tools"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/rivo/tview"
)

var (
	boolColors        = map[bool]string{true: "green", false: "red"}
	botRespMode       atomic.Bool
	toolRunningMode   atomic.Bool
	editMode          = false
	roleEditMode      = false
	injectRole        = true
	selectedIndex     = int(-1)
	shellMode         = false
	shellHistory      []string
	shellHistoryPos   int = -1
	thinkingCollapsed     = false

	toolModeShown   atomic.Bool
	statusLineTempl = "help (F12) | chat: [orange:-:b]%s[-:-:-] (F1) | [%s:-:b]tool use[-:-:-] (ctrl+k) | model: [%s:-:b]%s[-:-:-] (ctrl+l) | [%s:-:b]skip LLM resp[-:-:-] (F10) | API: [orange:-:b]%s[-:-:-] (ctrl+v)\nwriting as: [orange:-:b]%s[-:-:-] (ctrl+q) | bot will write as [orange:-:b]%s[-:-:-] (ctrl+x)"
	focusSwitcher   = map[tview.Primitive]tview.Primitive{}
	app             *tview.Application
	cliCardPath     string
	cliContinue     bool
	cliMsg          string
)

func main() {
	// parse flags
	flag.BoolVar(&cfg.CLIMode, "cli", false, "Run in CLI mode without TUI")
	flag.BoolVar(&cfg.ToolUse, "tools", true, "run with tools")
	flag.StringVar(&cfg.CurrentModel, "model", "modelname", "name of the model to use")
	flag.StringVar(&cliCardPath, "card", "", "Path to syscard JSON file")
	flag.BoolVar(&cliContinue, "continue", false, "Continue from last chat (by agent or card)")
	flag.StringVar(&cliMsg, "msg", "", "Send message and exit (one-shot mode)")
	flag.Parse()
	if !cfg.CLIMode {
		initTUI()
	}
	chatBody.Model = cfg.CurrentModel
	go updateModelLists()
	tools.InitTools(cfg, logger, store)
	// tooler = tools.InitTools(cfg, logger, store)
	// tooler.RegisterWindowTools(modelHasVision)
	if cfg.CLIMode {
		runCLIMode()
		return
	}
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(cfg.EnableMouse).EnablePaste(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}

func runCLIMode() {
	outputHandler = &CLIOutputHandler{}
	cliRespDone = make(chan bool, 1)
	if cliCardPath != "" {
		card, err := pngmeta.ReadCardJson(cliCardPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load syscard: %v\n", err)
			os.Exit(1)
		}
		cfg.AssistantRole = card.Role
		sysMap[card.ID] = card
		roleToID[card.Role] = card.ID
		charToStart(card.Role, false)
		fmt.Printf("Loaded syscard: %s (%s)\n", card.Role, card.FilePath)
	}
	if cliContinue {
		if cliCardPath != "" {
			history, err := loadAgentsLastChat(cfg.AssistantRole)
			if err != nil {
				fmt.Printf("No previous chat found for %s, starting new chat\n", cfg.AssistantRole)
				startNewCLIChat()
			} else {
				chatBody.Messages = history
				fmt.Printf("Continued chat: %s\n", activeChatName)
			}
		} else {
			chatBody.Messages = loadOldChatOrGetNew()
			fmt.Printf("Continued chat: %s\n", activeChatName)
		}
	} else {
		startNewCLIChat()
	}
	printCLIWelcome()
	go func() {
		<-ctx.Done()
		os.Exit(0)
	}()
	if cliMsg != "" {
		persona := cfg.UserRole
		if cfg.WriteNextMsgAs != "" {
			persona = cfg.WriteNextMsgAs
		}
		chatRoundChan <- &models.ChatRoundReq{Role: persona, UserMsg: cliMsg}
		<-cliRespDone
		fmt.Println()
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		msg := scanner.Text()
		if msg == "" {
			continue
		}
		if strings.HasPrefix(msg, "/") {
			if !handleCLICommand(msg) {
				return
			}
			fmt.Println()
			continue
		}
		persona := cfg.UserRole
		if cfg.WriteNextMsgAs != "" {
			persona = cfg.WriteNextMsgAs
		}
		chatRoundChan <- &models.ChatRoundReq{Role: persona, UserMsg: msg}
		<-cliRespDone
		fmt.Println()
	}
}

func printCLIWelcome() {
	fmt.Println("CLI Mode started. Type your messages or commands.")
	fmt.Println("Type /help for available commands.")
	fmt.Println()
}

func printCLIHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  /help, /h              - Show this help message")
	fmt.Println("  /new, /n               - Start a new chat (clears conversation)")
	fmt.Println("  /card <path>, /c <path> - Load a different syscard")
	fmt.Println("  /undo, /u              - Delete last message")
	fmt.Println("  /history, /ls          - List chat sessions")
	fmt.Println("  /hs [index]            - Show chat history (messages)")
	fmt.Println("  /load <name>           - Load a specific chat by name")
	fmt.Println("  /model <name>, /m <name> - Switch model")
	fmt.Println("  /api <index>, /a <index>  - Switch API link (no index to list)")
	fmt.Println("  /quit, /q, /exit       - Exit CLI mode")
	fmt.Println()
	fmt.Printf("Current syscard: %s\n", cfg.AssistantRole)
	fmt.Printf("Current model: %s\n", chatBody.Model)
	fmt.Printf("Current API: %s\n", cfg.CurrentAPI)
	fmt.Println()
}

func handleCLICommand(msg string) bool {
	parts := strings.Fields(msg)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/help", "/h":
		printCLIHelp()
	case "/new", "/n":
		startNewCLIChat()
		fmt.Println("New chat started.")
		fmt.Printf("Syscard: %s\n", cfg.AssistantRole)
		fmt.Println()
	case "/card", "/c":
		if len(args) == 0 {
			fmt.Println("Usage: /card <path>")
			return true
		}
		card, err := pngmeta.ReadCardJson(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load syscard: %v\n", err)
			return true
		}
		cfg.AssistantRole = card.Role
		sysMap[card.ID] = card
		roleToID[card.Role] = card.ID
		charToStart(card.Role, false)
		startNewCLIChat()
		fmt.Printf("Switched to syscard: %s (%s)\n", card.Role, card.FilePath)
	case "/undo", "/u":
		if len(chatBody.Messages) == 0 {
			fmt.Println("No messages to delete.")
			return true
		}
		chatBody.Messages = chatBody.Messages[:len(chatBody.Messages)-1]
		cliPrevOutput = ""
		fmt.Println("Last message deleted.")
	case "/history", "/ls":
		fmt.Println("Chat history:")
		for name := range chatMap {
			marker := "  "
			if name == activeChatName {
				marker = "* "
			}
			fmt.Printf("%s%s\n", marker, name)
		}
		fmt.Println()
	case "/load":
		if len(args) == 0 {
			fmt.Println("Usage: /load <name>")
			return true
		}
		name := args[0]
		chat, ok := chatMap[name]
		if !ok {
			fmt.Printf("Chat not found: %s\n", name)
			return true
		}
		history, err := chat.ToHistory()
		if err != nil {
			fmt.Printf("Failed to load chat: %v\n", err)
			return true
		}
		chatBody.Messages = history
		activeChatName = name
		cfg.AssistantRole = chat.Agent
		fmt.Printf("Loaded chat: %s\n", name)
	case "/hs":
		if len(chatBody.Messages) == 0 {
			fmt.Println("No messages in current chat.")
			return true
		}
		if len(args) == 0 {
			fmt.Println("Chat history:")
			for i := range chatBody.Messages {
				fmt.Printf("%d: %s\n", i, MsgToText(i, &chatBody.Messages[i]))
			}
			return true
		}
		idx, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Printf("Invalid index: %s\n", args[0])
			return true
		}
		if idx < 0 {
			idx = len(chatBody.Messages) + idx
		}
		if idx < 0 || idx >= len(chatBody.Messages) {
			fmt.Printf("Index out of range (0-%d)\n", len(chatBody.Messages)-1)
			return true
		}
		fmt.Printf("%d: %s\n", idx, MsgToText(idx, &chatBody.Messages[idx]))
	case "/model", "/m":
		getModelListForAPI := func(api string) []string {
			if strings.Contains(api, "api.deepseek.com/") {
				return []string{"deepseek-chat", "deepseek-reasoner"}
			} else if strings.Contains(api, "openrouter.ai") {
				return ORFreeModels
			}
			return LocalModels
		}
		modelList := getModelListForAPI(cfg.CurrentAPI)
		if len(args) == 0 {
			fmt.Println("Models:")
			for i, model := range modelList {
				marker := "  "
				if model == chatBody.Model {
					marker = "* "
				}
				fmt.Printf("%s%d: %s\n", marker, i, model)
			}
			fmt.Printf("\nCurrent model: %s\n", chatBody.Model)
			return true
		}
		// Try index first, then model name
		if idx, err := strconv.Atoi(args[0]); err == nil && idx >= 0 && idx < len(modelList) {
			chatBody.Model = modelList[idx]
			fmt.Printf("Switched to model: %s\n", chatBody.Model)
			return true
		}
		if slices.Index(modelList, args[0]) < 0 {
			fmt.Printf("Model '%s' not found. Use index or choose from:\n", args[0])
			for i, model := range modelList {
				fmt.Printf("  %d: %s\n", i, model)
			}
			return true
		}
		chatBody.Model = args[0]
		fmt.Printf("Switched to model: %s\n", args[0])
	case "/api", "/a":
		if len(args) == 0 {
			fmt.Println("API Links:")
			for i, link := range cfg.ApiLinks {
				marker := "  "
				if link == cfg.CurrentAPI {
					marker = "* "
				}
				fmt.Printf("%s%d: %s\n", marker, i, link)
			}
			fmt.Printf("\nCurrent API: %s\n", cfg.CurrentAPI)
			return true
		}
		idx := 0
		fmt.Sscanf(args[0], "%d", &idx)
		if idx < 0 || idx >= len(cfg.ApiLinks) {
			fmt.Printf("Invalid index. Valid range: 0-%d\n", len(cfg.ApiLinks)-1)
			return true
		}
		cfg.CurrentAPI = cfg.ApiLinks[idx]
		fmt.Printf("Switched to API: %s\n", cfg.CurrentAPI)
	case "/quit", "/q", "/exit":
		fmt.Println("Goodbye!")
		return false
	default:
		fmt.Printf("Unknown command: %s\n", msg)
		fmt.Println("Type /help for available commands.")
	}
	return true
}
