package storage

import (
	"elefant/models"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/jmoiron/sqlx"
)

func TestChatHistory(t *testing.T) {
	// Create an in-memory SQLite database
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open SQLite in-memory database: %v", err)
	}
	defer db.Close()
	// Create the chat table
	_, err = db.Exec(`
		CREATE TABLE chat (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    name TEXT NOT NULL,
	    msgs TEXT NOT NULL,  -- Store messages as a comma-separated string
	    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`)
	if err != nil {
		t.Fatalf("Failed to create chat table: %v", err)
	}
	// Initialize the ProviderSQL struct
	provider := ProviderSQL{db: db}
	// List chats (should be empty)
	chats, err := provider.ListChats()
	if err != nil {
		t.Fatalf("Failed to list chats: %v", err)
	}
	if len(chats) != 0 {
		t.Errorf("Expected 0 chats, got %d", len(chats))
	}
	// Upsert a chat
	chat := &models.Chat{
		ID:        1,
		Name:      "Test Chat",
		Msgs:      "Hello World",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	updatedChat, err := provider.UpsertChat(chat)
	if err != nil {
		t.Fatalf("Failed to upsert chat: %v", err)
	}
	if updatedChat == nil {
		t.Errorf("Expected non-nil chat after upsert")
	}
	// Get chat by ID
	fetchedChat, err := provider.GetChatByID(chat.ID)
	if err != nil {
		t.Fatalf("Failed to get chat by ID: %v", err)
	}
	if fetchedChat == nil {
		t.Errorf("Expected non-nil chat after get")
	}
	if fetchedChat.Name != chat.Name {
		t.Errorf("Expected chat name %s, got %s", chat.Name, fetchedChat.Name)
	}
	// List chats (should contain the upserted chat)
	chats, err = provider.ListChats()
	if err != nil {
		t.Fatalf("Failed to list chats: %v", err)
	}
	if len(chats) != 1 {
		t.Errorf("Expected 1 chat, got %d", len(chats))
	}
	// Remove chat
	err = provider.RemoveChat(chat.ID)
	if err != nil {
		t.Fatalf("Failed to remove chat: %v", err)
	}
	// List chats (should be empty again)
	chats, err = provider.ListChats()
	if err != nil {
		t.Fatalf("Failed to list chats: %v", err)
	}
	if len(chats) != 0 {
		t.Errorf("Expected 0 chats, got %d", len(chats))
	}
}
