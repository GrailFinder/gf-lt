package main

import (
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// showModelSelectionPopup creates a modal popup to select a model
func showModelSelectionPopup() {
	// Helper function to get model list for a given API
	getModelListForAPI := func(api string) []string {
		if strings.Contains(api, "api.deepseek.com/") {
			return []string{"deepseek-chat", "deepseek-reasoner"}
		} else if strings.Contains(api, "openrouter.ai") {
			return ORFreeModels
		}
		// Assume local llama.cpp
		refreshLocalModelsIfEmpty()
		localModelsMu.RLock()
		defer localModelsMu.RUnlock()
		return LocalModels
	}
	// Get the current model list based on the API
	modelList := getModelListForAPI(cfg.CurrentAPI)
	// Check for empty options list
	if len(modelList) == 0 {
		logger.Warn("empty model list for", "api", cfg.CurrentAPI, "localModelsLen", len(LocalModels), "orModelsLen", len(ORFreeModels))
		message := "No models available for selection"
		if strings.Contains(cfg.CurrentAPI, "openrouter.ai") {
			message = "No OpenRouter models available. Check token and connection."
		} else if strings.Contains(cfg.CurrentAPI, "api.deepseek.com") {
			message = "DeepSeek models should be available. Please report bug."
		} else {
			message = "No llama.cpp models loaded. Ensure llama.cpp server is running with models."
		}
		if err := notifyUser("Empty list", message); err != nil {
			logger.Error("failed to send notification", "error", err)
		}
		return
	}
	// Create a list primitive
	modelListWidget := tview.NewList().ShowSecondaryText(false).
		SetSelectedBackgroundColor(tcell.ColorGray)
	modelListWidget.SetTitle("Select Model").SetBorder(true)
	// Find the current model index to set as selected
	currentModelIndex := -1
	for i, model := range modelList {
		if model == chatBody.Model {
			currentModelIndex = i
		}
		modelListWidget.AddItem(model, "", 0, nil)
	}
	// Set the current selection if found
	if currentModelIndex != -1 {
		modelListWidget.SetCurrentItem(currentModelIndex)
	}
	modelListWidget.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		// Update the model in both chatBody and config
		chatBody.Model = mainText
		cfg.CurrentModel = chatBody.Model
		// Remove the popup page
		pages.RemovePage("modelSelectionPopup")
		// Update the status line to reflect the change
		updateStatusLine()
	})
	modelListWidget.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("modelSelectionPopup")
			return nil
		}
		return event
	})
	modal := func(p tview.Primitive, width, height int) tview.Primitive {
		return tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 1, true).
				AddItem(nil, 0, 1, false), width, 1, true).
			AddItem(nil, 0, 1, false)
	}
	// Add modal page and make it visible
	pages.AddPage("modelSelectionPopup", modal(modelListWidget, 80, 20), true, true)
	app.SetFocus(modelListWidget)
}

// showAPILinkSelectionPopup creates a modal popup to select an API link
func showAPILinkSelectionPopup() {
	// Prepare API links dropdown - ensure current API is in the list, avoid duplicates
	apiLinks := make([]string, 0, len(cfg.ApiLinks)+1)
	// Add current API first if it's not already in ApiLinks
	foundCurrentAPI := false
	for _, api := range cfg.ApiLinks {
		if api == cfg.CurrentAPI {
			foundCurrentAPI = true
		}
		apiLinks = append(apiLinks, api)
	}
	// If current API is not in the list, add it at the beginning
	if !foundCurrentAPI {
		apiLinks = make([]string, 0, len(cfg.ApiLinks)+1)
		apiLinks = append(apiLinks, cfg.CurrentAPI)
		apiLinks = append(apiLinks, cfg.ApiLinks...)
	}
	// Check for empty options list
	if len(apiLinks) == 0 {
		logger.Warn("no API links available for selection")
		message := "No API links available. Please configure API links in your config file."
		if err := notifyUser("Empty list", message); err != nil {
			logger.Error("failed to send notification", "error", err)
		}
		return
	}
	// Create a list primitive
	apiListWidget := tview.NewList().ShowSecondaryText(false).
		SetSelectedBackgroundColor(tcell.ColorGray)
	apiListWidget.SetTitle("Select API Link").SetBorder(true)
	// Find the current API index to set as selected
	currentAPIIndex := -1
	for i, api := range apiLinks {
		if api == cfg.CurrentAPI {
			currentAPIIndex = i
		}
		apiListWidget.AddItem(api, "", 0, nil)
	}
	// Set the current selection if found
	if currentAPIIndex != -1 {
		apiListWidget.SetCurrentItem(currentAPIIndex)
	}
	apiListWidget.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		// Update the API in config
		cfg.CurrentAPI = mainText
		// Update model list based on new API
		// Helper function to get model list for a given API (same as in props_table.go)
		getModelListForAPI := func(api string) []string {
			if strings.Contains(api, "api.deepseek.com/") {
				return []string{"deepseek-chat", "deepseek-reasoner"}
			} else if strings.Contains(api, "openrouter.ai") {
				return ORFreeModels
			}
			// Assume local llama.cpp
			refreshLocalModelsIfEmpty()
			localModelsMu.RLock()
			defer localModelsMu.RUnlock()
			return LocalModels
		}
		newModelList := getModelListForAPI(cfg.CurrentAPI)
		// Ensure chatBody.Model is in the new list; if not, set to first available model
		if len(newModelList) > 0 && !slices.Contains(newModelList, chatBody.Model) {
			chatBody.Model = newModelList[0]
			cfg.CurrentModel = chatBody.Model
		}
		// Remove the popup page
		pages.RemovePage("apiLinkSelectionPopup")
		// Update the parser and status line to reflect the change
		choseChunkParser()
		updateStatusLine()
	})
	apiListWidget.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("apiLinkSelectionPopup")
			return nil
		}
		return event
	})
	modal := func(p tview.Primitive, width, height int) tview.Primitive {
		return tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 1, true).
				AddItem(nil, 0, 1, false), width, 1, true).
			AddItem(nil, 0, 1, false)
	}
	// Add modal page and make it visible
	pages.AddPage("apiLinkSelectionPopup", modal(apiListWidget, 80, 20), true, true)
	app.SetFocus(apiListWidget)
}

// showUserRoleSelectionPopup creates a modal popup to select a user role
func showUserRoleSelectionPopup() {
	// Get the list of available roles
	roles := listRolesWithUser()
	// Check for empty options list
	if len(roles) == 0 {
		logger.Warn("no roles available for selection")
		message := "No roles available for selection."
		if err := notifyUser("Empty list", message); err != nil {
			logger.Error("failed to send notification", "error", err)
		}
		return
	}
	// Create a list primitive
	roleListWidget := tview.NewList().ShowSecondaryText(false).
		SetSelectedBackgroundColor(tcell.ColorGray)
	roleListWidget.SetTitle("Select User Role").SetBorder(true)
	// Find the current role index to set as selected
	currentRole := cfg.UserRole
	if cfg.WriteNextMsgAs != "" {
		currentRole = cfg.WriteNextMsgAs
	}
	currentRoleIndex := -1
	for i, role := range roles {
		if strings.EqualFold(role, currentRole) {
			currentRoleIndex = i
		}
		roleListWidget.AddItem(role, "", 0, nil)
	}
	// Set the current selection if found
	if currentRoleIndex != -1 {
		roleListWidget.SetCurrentItem(currentRoleIndex)
	}
	roleListWidget.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		// Update the user role in config
		cfg.WriteNextMsgAs = mainText
		// role got switch, update textview with character specific context for user
		filtered := filterMessagesForCharacter(chatBody.Messages, mainText)
		textView.SetText(chatToText(filtered, cfg.ShowSys))
		// Remove the popup page
		pages.RemovePage("userRoleSelectionPopup")
		// Update the status line to reflect the change
		updateStatusLine()
		colorText()
	})
	roleListWidget.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("userRoleSelectionPopup")
			return nil
		}
		return event
	})
	modal := func(p tview.Primitive, width, height int) tview.Primitive {
		return tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 1, true).
				AddItem(nil, 0, 1, false), width, 1, true).
			AddItem(nil, 0, 1, false)
	}
	// Add modal page and make it visible
	pages.AddPage("userRoleSelectionPopup", modal(roleListWidget, 80, 20), true, true)
	app.SetFocus(roleListWidget)
}

// showBotRoleSelectionPopup creates a modal popup to select a bot role
func showBotRoleSelectionPopup() {
	// Get the list of available roles
	roles := listChatRoles()
	if len(roles) == 0 {
		logger.Warn("empty roles in chat")
	}
	if !strInSlice(cfg.AssistantRole, roles) {
		roles = append(roles, cfg.AssistantRole)
	}
	// Check for empty options list
	if len(roles) == 0 {
		logger.Warn("no roles available for selection")
		message := "No roles available for selection."
		if err := notifyUser("Empty list", message); err != nil {
			logger.Error("failed to send notification", "error", err)
		}
		return
	}
	// Create a list primitive
	roleListWidget := tview.NewList().ShowSecondaryText(false).
		SetSelectedBackgroundColor(tcell.ColorGray)
	roleListWidget.SetTitle("Select Bot Role").SetBorder(true)
	// Find the current role index to set as selected
	currentRole := cfg.AssistantRole
	if cfg.WriteNextMsgAsCompletionAgent != "" {
		currentRole = cfg.WriteNextMsgAsCompletionAgent
	}
	currentRoleIndex := -1
	for i, role := range roles {
		if strings.EqualFold(role, currentRole) {
			currentRoleIndex = i
		}
		roleListWidget.AddItem(role, "", 0, nil)
	}
	// Set the current selection if found
	if currentRoleIndex != -1 {
		roleListWidget.SetCurrentItem(currentRoleIndex)
	}
	roleListWidget.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		// Update the bot role in config
		cfg.WriteNextMsgAsCompletionAgent = mainText
		// Remove the popup page
		pages.RemovePage("botRoleSelectionPopup")
		// Update the status line to reflect the change
		updateStatusLine()
	})
	roleListWidget.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("botRoleSelectionPopup")
			return nil
		}
		return event
	})
	modal := func(p tview.Primitive, width, height int) tview.Primitive {
		return tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 1, true).
				AddItem(nil, 0, 1, false), width, 1, true).
			AddItem(nil, 0, 1, false)
	}
	// Add modal page and make it visible
	pages.AddPage("botRoleSelectionPopup", modal(roleListWidget, 80, 20), true, true)
	app.SetFocus(roleListWidget)
}
