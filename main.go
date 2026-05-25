package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"gf-lt/mcp"
	"gf-lt/mission"
	"gf-lt/models"
	"gf-lt/pngmeta"
	"gf-lt/tools"
	"os"
	"os/signal"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rivo/tview"
)

var (
	missionAgentCardPath string
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

	toolModeShown     atomic.Bool
	statusLineTempl   = "help (F12) | chat: [orange:-:b]%s[-:-:-] (F1) | [%s:-:b]tool use[-:-:-] (ctrl+k) | model: [%s:-:b]%s[-:-:-] (ctrl+l) | [%s:-:b]skip LLM resp[-:-:-] (F10) | API: [orange:-:b]%s[-:-:-] (ctrl+v)\nwriting as: [orange:-:b]%s[-:-:-] (ctrl+q) | bot will write as [orange:-:b]%s[-:-:-] (ctrl+x)"
	focusSwitcher     = map[tview.Primitive]tview.Primitive{}
	app               *tview.Application
	cliCardPath       string
	cliContinue       bool
	cliMsg            string
	mcpManager        *mcp.Manager
	missionResumeFile string
	missionAgentCard  string
	missionIssueID    string
	missionCheckpoint string
	missionSummarizeFailures int // track consecutive summarization failures
)

func main() {
	// parse flags
	flag.BoolVar(&cfg.CLIMode, "cli", false, "Run in CLI mode without TUI")
	flag.BoolVar(&cfg.ToolUse, "tools", true, "run with tools")
	flag.StringVar(&cfg.CurrentModel, "model", "auto", "name of the model to use (default: auto, overridden by GF_LT_MODEL env if set)")
	flag.StringVar(&cliCardPath, "card", "", "Path to syscard JSON file")
	flag.BoolVar(&cliContinue, "continue", false, "Continue from last chat (by agent or card)")
	flag.StringVar(&cliMsg, "msg", "", "Send message and exit (one-shot mode)")
	flag.BoolVar(&cfg.MissionMode, "mission", false, "Run in mission mode (auto issue solver)")
	flag.StringVar(&missionIssueID, "issue-id", "", "Issue ID to process in mission mode")
	flag.StringVar(&missionAgentCard, "agent-card", "", "Path to agent card for mission mode")
	flag.StringVar(&missionResumeFile, "resume", "", "Resume mission from checkpoint file")
	flag.StringVar(&missionCheckpoint, "checkpoint-file", "", "Custom checkpoint file path")
	flag.IntVar(&cfg.MissionPMInterval, "pm-interval", 75, "PM check-in interval (tool calls)")
	flag.IntVar(&cfg.MissionMaxFailures, "max-failures", 3, "Max consecutive failures before abort")
	flag.StringVar(&cfg.MissionOutputFormat, "output", "text", "Output format: text or json")
	flag.BoolVar(&cfg.MissionQuiet, "quiet", false, "Suppress tool call logging in mission mode")
	flag.BoolVar(&cfg.MissionToolsEnabled, "mission-tools", false, "Enable mission tools (move_issue, create_pr, etc.) in non-mission mode")
	flag.StringVar(&cfg.IssuesDir, "issues-dir", "auto", "Directory containing issues (default: ./issues, overridden by GF_LT_ISSUES_DIR env if set)")
	flag.StringVar(&cfg.CurrentAPI, "api", "", "Override API endpoint (default: from config.toml)")
	flag.Parse()

	// Restore config.toml ChatAPI if --api flag wasn't explicitly set
	if cfg.CurrentAPI == "" {
		cfg.CurrentAPI = cfg.ChatAPI
	}

	if cfg.MissionMode {
		cfg.CLIMode = true
	}

	// Priority: -model flag > GF_LT_MODEL env > "auto"
	if cfg.CurrentModel == "auto" {
		if envModel := os.Getenv("GF_LT_MODEL"); envModel != "" {
			cfg.CurrentModel = envModel
		}
	}

	// Priority: --issues-dir flag > GF_LT_ISSUES_DIR env > "./issues"
	if cfg.IssuesDir == "auto" {
		if envDir := os.Getenv("GF_LT_ISSUES_DIR"); envDir != "" {
			cfg.IssuesDir = envDir
		} else {
			cfg.IssuesDir = "./issues"
		}
	}
	chatBody.Model = cfg.CurrentModel
	go updateModelLists()
	tools.InitTools(cfg, logger, store)
	if cfg.ToolUse && len(cfg.MCPServers) > 0 {
		mcpManager = mcp.NewManager(cfg, logger)
		if err := mcpManager.ConnectAll(context.Background()); err != nil {
			logger.Error("failed to connect to MCP servers", "error", err)
		} else {
			mcpManager.RegisterToolHandlers(tools.FnMap)
		}
	}
	_ = mcpManager
	// Route to appropriate mode
	if cfg.MissionMode {
		tools.RegisterMissionTools()
		cfg.MissionToolsEnabled = true
		runMissionMode()
		return
	}
	if cfg.CLIMode {
		runCLIMode()
		return
	}
	// TUI mode
	if cfg.MissionToolsEnabled {
		tools.RegisterMissionTools()
	}
	initTUI()
	pages.AddPage("main", flex, true, true)
	if err := app.SetRoot(pages,
		true).EnableMouse(cfg.EnableMouse).EnablePaste(true).Run(); err != nil {
		logger.Error("failed to start tview app", "error", err)
		return
	}
}

func setupSignalHandler() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\n[signal] %v received, exporting chat...\n", sig)

		issueID := "interrupted"
		if m := tools.GetCurrentMission(); m != nil {
			issueID = m.Issue.ID
			m.Status = mission.StatusAborted
			m.SaveCheckpoint(mission.DefaultCheckpointPath())
		}

		exportMissionChat(issueID)

		code := 130 // 128 + SIGINT(2)
		if sig == syscall.SIGTERM {
			code = 143 // 128 + SIGTERM(15)
		}
		os.Exit(code)
	}()
}

func runCLIMode() {
	setupSignalHandler()
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
		idx, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Printf("Invalid index: %s\n", args[0])
			return true
		}
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

func runMissionMode() {
	setupSignalHandler()
	outputHandler = &CLIOutputHandler{}
	cliRespDone = make(chan bool, 1)

	checkpointPath := missionCheckpoint
	if checkpointPath == "" {
		checkpointPath = mission.DefaultCheckpointPath()
	}

	var resumeFrom *mission.Checkpoint
	var issue *mission.Issue
	var issueStatus mission.IssueStatus
	var err error

	issueManager := mission.NewIssueManager(cfg.IssuesDir)

	if missionResumeFile != "" {
		resumeFrom, err = mission.LoadCheckpoint(missionResumeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load checkpoint: %v\n", err)
			os.Exit(1)
		}
		issue, issueStatus, err = issueManager.LoadByIDAny(resumeFrom.IssueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load issue from checkpoint: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Resumed mission for issue %s (status: %s)\n", issue.ID, issueStatus)
	} else {
		if missionIssueID != "" {
			issue, issueStatus, err = issueManager.LoadByIDAny(missionIssueID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load issue %s: %v\n", missionIssueID, err)
				os.Exit(1)
			}
			fmt.Printf("Loaded issue %s (status: %s)\n", issue.ID, issueStatus)
		} else {
			issue, issueStatus, err = issueManager.PickOpenIssue()
			if err != nil {
				fmt.Fprintf(os.Stderr, "No open issues found: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Auto-selected issue %s\n", issue.ID)
		}

		if issueStatus == mission.StatusOpen {
			if err := issueManager.MoveIssue(issue.ID, mission.StatusOpen, mission.StatusInProgress); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to move issue to in_progress: %v\n", err)
				os.Exit(1)
			}
			issue.Status = mission.StatusInProgress
			fmt.Printf("Issue moved to in_progress\n")
		}
	}

	m := mission.NewMission(issue, issueManager, cfg.MissionPMInterval, cfg.MissionMaxFailures, cfg.MissionQuiet)

	if resumeFrom != nil {
		m.Checkpoint = resumeFrom
	}

	// Load agent card if provided
	var agentSysprompt string
	if missionAgentCard != "" {
		card, err := pngmeta.ReadCardJson(missionAgentCard)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load agent card: %v\n", err)
			os.Exit(1)
		}
		agentSysprompt = card.SysPrompt
		cfg.AssistantRole = card.Role
		fmt.Printf("Loaded agent card: %s\n", card.Role)
	} else {
		// Use default auto-solver card
		defaultCardPath := "sysprompts/auto-solver-default.json"
		if card, err := pngmeta.ReadCardJson(defaultCardPath); err == nil {
			agentSysprompt = card.SysPrompt
			cfg.AssistantRole = card.Role
			fmt.Printf("Using default agent card: %s\n", card.Role)
		}
	}

	if !cfg.MissionQuiet {
		fmt.Println("\n=== Mission Started ===")
		fmt.Printf("Issue: %s - %s\n", issue.ID, issue.Title)
		fmt.Printf("Project: %s\n", issue.ProjectPath)
		fmt.Printf("PM Interval: %d tool calls\n", cfg.MissionPMInterval)
		fmt.Printf("Max Failures: %d\n\n", cfg.MissionMaxFailures)
	}

	runMission(m, checkpointPath, agentSysprompt)
}

func runMission(m *mission.Mission, checkpointPath string, agentSysprompt string) {
	startTime := time.Now()
	m.Status = mission.StatusRunning
	cfg.CLIMode = true // Use CLI mode infrastructure

	if err := m.SaveCheckpoint(checkpointPath); err != nil {
		m.Log("Warning: failed to save initial checkpoint: %v", err)
	}

	tools.SetCurrentMission(m)

	// Set token callback for agent-based LLM calls (PM agent, etc.)
	tools.SetTokenFunc(func() string {
		choseChunkParser()
		return chunkParser.GetToken()
	})

	outputHandler = &CLIOutputHandler{}
	cliRespDone = make(chan bool, 1)
	chatBody.Model = cfg.CurrentModel
	tools.InitTools(cfg, logger, store)
	tools.InitPMAgent(cfg, logger)
	startNewCLIChat()

	// Set working directory to issue's project path
	if m.Issue.ProjectPath != "" {
		if err := tools.SetFSCwd(m.Issue.ProjectPath); err != nil {
			m.Log("Warning: failed to set CWD to %s: %v", m.Issue.ProjectPath, err)
		} else {
			m.Log("Working directory set to: %s", m.Issue.ProjectPath)
		}
	}

	// Build initial system message with agent prompt + issue context
	workflowDocs := ""
	if wf, err := os.ReadFile("docs/issue-workflow.md"); err == nil {
		workflowDocs = "\n\n## Issue Workflow Guidelines:\n" + string(wf)
	}

	systemMsg := fmt.Sprintf(
		"%s\n\n## Current Issue\n\nIssue ID: %s\nTitle: %s\n\nDescription:\n%s\n\nProject path: %s\n\nAcceptance criteria:\n- %s%s",
		agentSysprompt,
		m.Issue.ID,
		m.Issue.Title,
		m.Issue.Description,
		m.Issue.ProjectPath,
		strings.Join(m.Issue.AcceptanceCriteria, "\n- "),
		workflowDocs,
	)

	// Add initial context messages
	m.AddToConversation("system", systemMsg)
	chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
		Role: "system", Content: systemMsg,
	})
	// Add assistant "ready" message to trigger LLM
	m.AddToConversation("assistant", "I'm ready to solve this issue. Let me start by examining the codebase and understanding the current structure before implementing the solution.")
	chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
		Role: cfg.AssistantRole, Content: "I'm ready to solve this issue. Let me start by examining the codebase and understanding the current structure before implementing the solution.",
	})

	// Start the message loop
	go missionMessageLoop(m, checkpointPath, startTime)

	// Block until context done (interrupt) or mission completes
	<-ctx.Done()
}

func missionMessageLoop(m *mission.Mission, checkpointPath string, startTime time.Time) {
	// Send the first user prompt to start the conversation
	chatRoundChan <- &models.ChatRoundReq{
		Role:    cfg.UserRole,
		UserMsg: "Proceed with the issue. What is your next step?",
	}

	var emptyRespRetries int
	missionSummarizeFailures = 0 // reset at mission start

	for {
		select {
		case <-cliRespDone:
			// Detect empty responses — silently retry up to 3 times
			if isLastAssistantMsgEmpty() {
				emptyRespRetries++
				if emptyRespRetries < 3 {
					m.Log("Empty response, retrying silently (%d/3)", emptyRespRetries)
					chatRoundChan <- &models.ChatRoundReq{Role: cfg.AssistantRole}
					continue
				}
				emptyRespRetries = 0
				m.AddFailure()
				m.Log("Empty response limit reached (3/3), counting as failure %d/%d",
					m.Checkpoint.ConsecutiveFailures, m.MaxFailures)
			} else {
				emptyRespRetries = 0
			}

			// Response complete - check for mission completion signals
			m.Log("Response complete. Tool calls: %d, Failures: %d",
				m.Checkpoint.ToolCallCount, m.Checkpoint.ConsecutiveFailures)

			// Check for create_pr tool completion
			if m.Status == mission.StatusSuccess {
				missionComplete(m, checkpointPath, mission.StatusSuccess, startTime)
				return
			}

			// Check failure threshold
			if m.ShouldAbort() {
				m.Log("Maximum failures reached (%d), aborting mission", m.Checkpoint.ConsecutiveFailures)
				m.Status = mission.StatusFailed
				missionComplete(m, checkpointPath, mission.StatusFailed, startTime)
				return
			}

			// Context window management — compact if > 90% saturation
			summarizeAndCompact()

			// If summarization failed repeatedly and context is saturated, abort
			if missionSummarizeFailures >= 3 {
				maxCtx := getMaxContextTokens()
				if maxCtx == 0 {
					maxCtx = 16384
				}
				if float64(getContextTokens())/float64(maxCtx) >= 0.9 {
					m.Log("Context window saturated and summarization failed 3x, aborting mission")
					m.Status = mission.StatusFailed
					missionComplete(m, checkpointPath, mission.StatusFailed, startTime)
					return
				}
			}

			// PM check-in at interval
			if m.ShouldPMCheckIn() {
				m.Log("PM check-in triggered at tool call %d", m.Checkpoint.ToolCallCount)
				// PM response will be injected as a message
				pmResponse := getPMGuidance(m)
				m.AddToConversation("system", fmt.Sprintf("[PM Check-in]\n%s", pmResponse))
				chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
					Role: "system", Content: fmt.Sprintf("[PM Check-in]\n%s", pmResponse),
				})
				// Send PM check-in as assistant message to trigger LLM
				chatRoundChan <- &models.ChatRoundReq{Role: cfg.AssistantRole}
				continue
			}

			// Continue conversation - ask for next action
			m.AddToConversation("user", "Continue working on the issue. What is your next step?")
			chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
				Role: cfg.UserRole, Content: "Continue working on the issue. What is your next step?",
			})
			chatRoundChan <- &models.ChatRoundReq{Role: cfg.UserRole, UserMsg: "Continue working on the issue. What is your next step?"}

		case <-ctx.Done():
			m.Log("Mission interrupted")
			m.Status = mission.StatusAborted
			m.SaveCheckpoint(checkpointPath)
			return
		}
	}
}

func summarizeAndCompact() {
	contextTokens := getContextTokens()
	maxCtx := getMaxContextTokens()
	if maxCtx == 0 {
		maxCtx = 16384
	}
	if contextTokens == 0 || float64(contextTokens)/float64(maxCtx) < 0.9 {
		return
	}

	messages := chatBody.Messages
	if len(messages) < 20 {
		return
	}

	keep := 15
	split := len(messages) - keep
	if split < 3 {
		return
	}

	toSummarize := messages[:split]
	toKeep := messages[split:]

	summary, err := tools.SummarizeChat(toSummarize)
	if err != nil || strings.TrimSpace(summary) == "" {
		missionSummarizeFailures++
		logger.Warn("context summarization failed, continuing without compression", "error", err, "consecutive_failures", missionSummarizeFailures)
		return
	}
	missionSummarizeFailures = 0 // reset on success

	summaryMsg := models.RoleMsg{
		Role:    "system",
		Content: fmt.Sprintf("[Context summary of previous conversation]\n%s", summary),
	}

	chatBody.Messages = append([]models.RoleMsg{summaryMsg}, toKeep...)
	logger.Info("context compressed", "summarized", split, "messages_kept", keep, "summary_len", len(summary))
}

func isLastAssistantMsgEmpty() bool {
	for i := len(chatBody.Messages) - 1; i >= 0; i-- {
		msg := chatBody.Messages[i]
		if msg.Role != cfg.AssistantRole && msg.Role != "assistant" {
			continue
		}
		// Found the last assistant message - check if it's empty
		if msg.ToolCall != nil {
			return false
		}
		if len(msg.ToolCalls) > 0 {
			return false
		}
		if msg.HasContentParts {
			return len(msg.ContentParts) == 0
		}
		return strings.TrimSpace(msg.Content) == ""
	}
	return false
}

func getPMGuidance(m *mission.Mission) string {
	ac := "N/A"
	if len(m.Issue.AcceptanceCriteria) > 0 {
		ac = "- " + strings.Join(m.Issue.AcceptanceCriteria, "\n- ")
	}
	msg := fmt.Sprintf(
		"PM check-in point. Assess the agent's progress.\n\n"+
			"Issue: %s (%s)\n"+
			"Description: %s\n"+
			"Acceptance criteria:\n%s\n"+
			"Branch: %s\nTool calls: %d\nCommits: %v\nConsecutive failures: %d\n\n"+
			"Is the agent on track? What should it focus on next?",
		m.Issue.Title, m.Issue.ID, m.Issue.Description,
		ac,
		m.Issue.BranchName,
		m.Checkpoint.ToolCallCount, m.Checkpoint.CommitsMade,
		m.Checkpoint.ConsecutiveFailures,
	)
	return tools.PMAgentChat(msg)
}

func exportMissionChat(issueID string) {
	data, err := json.MarshalIndent(chatBody.Messages, "", "  ")
	if err != nil {
		logger.Error("failed to marshal mission chat", "error", err)
		return
	}
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		logger.Error("failed to create export dir", "error", err)
		return
	}
	ts := time.Now().Format("20060102-150405")
	fp := path.Join(exportDir, fmt.Sprintf("mission-%s-%s.json", issueID, ts))
	if err := os.WriteFile(fp, data, 0666); err != nil {
		logger.Error("failed to write mission chat export", "error", err)
		return
	}
	fmt.Printf("Chat exported to: %s\n", fp)
}

func missionComplete(m *mission.Mission, checkpointPath string, status mission.MissionStatus, startTime time.Time) {
	// Export chat for analysis
	exportMissionChat(m.Issue.ID)

	duration := time.Since(startTime)

	// Save final checkpoint
	m.Status = status
	m.SaveCheckpoint(checkpointPath)

	// Move issue to appropriate status
	switch status {
	case mission.StatusSuccess:
		m.MoveToStatus(mission.StatusReview)
	case mission.StatusFailed, mission.StatusAborted:
		m.MoveToStatus(mission.StatusArchive)
	}

	if !cfg.MissionQuiet {
		fmt.Println("\n=== Mission " + string(status) + " ===")
		fmt.Printf("Issue: %s\n", m.Issue.ID)
		fmt.Printf("Tool calls: %d\n", m.Checkpoint.ToolCallCount)
		fmt.Printf("Commits: %d\n", len(m.Checkpoint.CommitsMade))
		fmt.Printf("Duration: %s\n", duration)
	}

	if cfg.MissionOutputFormat == "json" {
		result := mission.MissionResult{
			Status:     status,
			IssueID:    m.Issue.ID,
			BranchName: m.Issue.BranchName,
			Commits:    m.Checkpoint.CommitsMade,
			ToolCalls:  m.Checkpoint.ToolCallCount,
			Duration:   duration,
		}
		fmt.Println(result.ToJSON())
	}

	if status == mission.StatusSuccess {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
