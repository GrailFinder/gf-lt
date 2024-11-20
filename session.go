package main

import (
	"elefant/models"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var (
	chatMap = make(map[string]*models.Chat)
)

func historyToSJSON(msgs []models.MessagesStory) (string, error) {
	data, err := json.Marshal(msgs)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", fmt.Errorf("nil data")
	}
	return string(data), nil
}

func updateStorageChat(name string, msgs []models.MessagesStory) error {
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
	_, err = store.UpsertChat(chat)
	return err
}

func loadHistoryChats() ([]string, error) {
	chats, err := store.ListChats()
	if err != nil {
		return nil, err
	}
	resp := []string{}
	for _, chat := range chats {
		if chat.Name == "" {
			chat.Name = fmt.Sprintf("%d_%v", chat.ID, chat.CreatedAt.Unix())
		}
		resp = append(resp, chat.Name)
		chatMap[chat.Name] = &chat
	}
	return resp, nil
}

func loadHistoryChat(chatName string) ([]models.MessagesStory, error) {
	chat, ok := chatMap[chatName]
	if !ok {
		err := fmt.Errorf("failed to read chat")
		logger.Error("failed to read chat", "name", chatName)
		return nil, err
	}
	activeChatName = chatName
	return chat.ToHistory()
}

func loadOldChatOrGetNew() []models.MessagesStory {
	// find last chat
	chat, err := store.GetLastChat()
	newChat := &models.Chat{
		ID:        0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	newChat.Name = fmt.Sprintf("%d_%v", chat.ID, chat.CreatedAt.Unix())
	if err != nil {
		logger.Warn("failed to load history chat", "error", err)
		activeChatName = newChat.Name
		chatMap[newChat.Name] = newChat
		return defaultStarter
	}
	history, err := chat.ToHistory()
	if err != nil {
		logger.Warn("failed to load history chat", "error", err)
		activeChatName = newChat.Name
		chatMap[newChat.Name] = newChat
		return defaultStarter
	}
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
