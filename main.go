package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"gf-lt/mcp"
	"gf-lt/mission"
	"gf-lt/models"
	"gf-lt/pngmeta"
	"gf-lt/tools"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
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

	toolModeShown   atomic.Bool
	statusLineTempl = "help (F12) | chat: [orange:-:b]%s[-:-:-] (F1) | [%s:-:b]tool use[-:-:-] (ctrl+k) | model: [%s:-:b]%s[-:-:-] (ctrl+l) | [%s:-:b]skip LLM resp[-:-:-] (F10) | API: [orange:-:b]%s[-:-:-] (ctrl+v)\nwriting as: [orange:-:b]%s[-:-:-] (ctrl+q) | bot will write as [orange:-:b]%s[-:-:-] (ctrl+x)"
	focusSwitcher   = map[tview.Primitive]tview.Primitive{}
	app             *tview.Application
	cliCardPath     string
	cliContinue     bool
	cliMsg          string
	mcpManager      *mcp.Manager
	missionResumeFile   string
	missionAgentCard    string
	missionIssueID      string
	missionCheckpoint   string
)

func main() {
	// parse flags
	flag.BoolVar(&cfg.CLIMode, "cli", false, "Run in CLI mode without TUI")
	flag.BoolVar(&cfg.ToolUse, "tools", true, "run with tools")
	flag.StringVar(&cfg.CurrentModel, "model", "modelname", "name of the model to use")
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
	flag.StringVar(&cfg.IssuesDir, "issues-dir", "", "Directory containing issues (default: ./issues)")
	flag.Parse()
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

	outputHandler = &CLIOutputHandler{}
	cliRespDone = make(chan bool, 1)
	chatBody.Model = cfg.CurrentModel
	tools.InitTools(cfg, logger, store)
	startNewCLIChat()

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
	// Send the first message to start the conversation
	chatRoundChan <- &models.ChatRoundReq{
		Role: cfg.AssistantRole,
	}

	for {
		select {
		case <-cliRespDone:
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

func getPMGuidance(m *mission.Mission) string {
	// Simple PM guidance based on progress
	toolCalls := m.Checkpoint.ToolCallCount
	commits := len(m.Checkpoint.CommitsMade)
	failures := m.Checkpoint.ConsecutiveFailures

	context := fmt.Sprintf(
		"Issue: %s (%s)\nBranch: %s\nProgress: %d tool calls, %d commits made\nConsecutive failures: %d",
		m.Issue.Title, m.Issue.ID, m.Issue.BranchName, toolCalls, commits, failures,
	)

	if failures > 0 {
		return fmt.Sprintf("%s\n\nWARNING: You have %d consecutive failures. Consider:\n- Reviewing the error messages\n- Asking for clarification with pm_consult\n- Trying a different approach\n- Breaking down the problem into smaller steps", context, failures)
	}

	if commits == 0 && toolCalls > 20 {
		return fmt.Sprintf("%s\n\nYou haven't made any commits yet. Consider:\n- Creating a feature branch\n- Starting with a small, focused change\n- Running tests to verify the environment", context)
	}

	if commits > 0 && commits < 3 {
		return fmt.Sprintf("%s\n\nGood progress! You've made %d commit(s). Keep implementing incrementally and test each change.", context, commits)
	}

	if commits >= 3 {
		return fmt.Sprintf("%s\n\nExcellent progress! %d commits made. Focus on:\n- Ensuring all acceptance criteria are met\n- Writing or updating tests\n- Preparing the PR description", context, commits)
	}

	return fmt.Sprintf("%s\n\nRegular check-in. Keep working systematically through the issue.", context)
}

func missionComplete(m *mission.Mission, checkpointPath string, status mission.MissionStatus, startTime time.Time) {
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
			Status:    status,
			IssueID:   m.Issue.ID,
			BranchName: m.Issue.BranchName,
			Commits:   m.Checkpoint.CommitsMade,
			ToolCalls: m.Checkpoint.ToolCallCount,
			Duration:  duration,
		}
		fmt.Println(result.ToJSON())
	}

	if status == mission.StatusSuccess {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
