package main

import (
	"elefant/models"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	chatMap = make(map[string]*models.Chat)
)

func historyToSJSON(msgs []models.RoleMsg) (string, error) {
	data, err := json.Marshal(msgs)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", errors.New("nil data")
	}
	return string(data), nil
}

func exportChat() error {
	data, err := json.MarshalIndent(chatBody.Messages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(activeChatName+".json", data, 0666)
}

func importChat(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	messages := []models.RoleMsg{}
	if err := json.Unmarshal(data, &messages); err != nil {
		return err
	}
	activeChatName = filepath.Base(filename)
	chatBody.Messages = messages
	cfg.AssistantRole = messages[1].Role
	if cfg.AssistantRole == cfg.UserRole {
		cfg.AssistantRole = messages[2].Role
	}
	return nil
}

func updateStorageChat(name string, msgs []models.RoleMsg) error {
	var err error
	chat, ok := chatMap[name]
	if !ok {
		err = fmt.Errorf("failed to find active chat; map:%v; key:%s", chatMap, name)
		logger.Error("failed to find active chat", "map", chatMap, "key", name)
		return err
	}
	chat.Msgs, err = historyToSJSON(msgs)
	if err != nil {
		return err
	}
	chat.UpdatedAt = time.Now()
	// if new chat will create id
	_, err = store.UpsertChat(chat)
	return err
}

func loadHistoryChats() ([]string, error) {
	chats, err := store.ListChats()
	if err != nil {
		return nil, err
	}
	resp := make([]string, len(chats))
	for i, chat := range chats {
		if chat.Name == "" {
			chat.Name = fmt.Sprintf("%d_%v", chat.ID, chat.Agent)
		}
		resp[i] = chat.Name
		chatMap[chat.Name] = &chat
	}
	return resp, nil
}

func loadHistoryChat(chatName string) ([]models.RoleMsg, error) {
	chat, ok := chatMap[chatName]
	if !ok {
		err := errors.New("failed to read chat")
		logger.Error("failed to read chat", "name", chatName)
		return nil, err
	}
	activeChatName = chatName
	cfg.AssistantRole = chat.Agent
	return chat.ToHistory()
}

func loadAgentsLastChat(agent string) ([]models.RoleMsg, error) {
	chat, err := store.GetLastChatByAgent(agent)
	if err != nil {
		return nil, err
	}
	history, err := chat.ToHistory()
	if err != nil {
		return nil, err
	}
	if chat.Name == "" {
		logger.Warn("empty chat name", "id", chat.ID)
		chat.Name = fmt.Sprintf("%s_%d", chat.Agent, chat.ID)
	}
	chatMap[chat.Name] = chat
	activeChatName = chat.Name
	return history, nil
}

func loadOldChatOrGetNew() []models.RoleMsg {
	// find last chat
	chat, err := store.GetLastChat()
	if err != nil {
		logger.Warn("failed to load history chat", "error", err)
		chat := &models.Chat{
			ID:        0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Agent:     cfg.AssistantRole,
		}
		chat.Name = fmt.Sprintf("%s_%v", chat.Agent, chat.CreatedAt.Unix())
		activeChatName = chat.Name
		chatMap[chat.Name] = chat
		return defaultStarter
	}
	history, err := chat.ToHistory()
	if err != nil {
		logger.Warn("failed to load history chat", "error", err)
		activeChatName = chat.Name
		chatMap[chat.Name] = chat
		return defaultStarter
	}
	// if chat.Name == "" {
	// 	logger.Warn("empty chat name", "id", chat.ID)
	// 	chat.Name = fmt.Sprintf("%s_%v", chat.Agent, chat.CreatedAt.Unix())
	// }
	chatMap[chat.Name] = chat
	activeChatName = chat.Name
	cfg.AssistantRole = chat.Agent
	return history
}

func copyToClipboard(text string) error {
	cmd := exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func notifyUser(topic, message string) error {
	cmd := exec.Command("notify-send", topic, message)
	return cmd.Run()
}
