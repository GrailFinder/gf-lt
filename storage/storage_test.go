package storage

import (
	"elefant/models"
	"fmt"
	"log"
	"log/slog"
	"os"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/glebarez/go-sqlite"
	"github.com/jmoiron/sqlx"
	"github.com/ncruces/go-sqlite3"
)

func TestMemories(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open SQLite in-memory database: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS memories (
    agent TEXT NOT NULL,
    topic TEXT NOT NULL,
    mind TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (agent, topic)
);`)
	if err != nil {
		t.Fatalf("Failed to create chat table: %v", err)
	}
	provider := ProviderSQL{
		db:     db,
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
	// Create a sample memory for testing
	sampleMemory := &models.Memory{
		Agent:     "testAgent",
		Topic:     "testTopic",
		Mind:      "testMind",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	cases := []struct {
		memory *models.Memory
	}{
		{memory: sampleMemory},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("run_%d", i), func(t *testing.T) {
			// Recall topics: get no rows
			topics, err := provider.RecallTopics(tc.memory.Agent)
			if err != nil {
				t.Fatalf("Failed to recall topics: %v", err)
			}
			if len(topics) != 0 {
				t.Fatalf("Expected no topics, got: %v", topics)
			}
			// Memorise
			_, err = provider.Memorise(tc.memory)
			if err != nil {
				t.Fatalf("Failed to memorise: %v", err)
			}
			// Recall topics: has topics
			topics, err = provider.RecallTopics(tc.memory.Agent)
			if err != nil {
				t.Fatalf("Failed to recall topics: %v", err)
			}
			if len(topics) == 0 {
				t.Fatalf("Expected topics, got none")
			}
			// Recall
			content, err := provider.Recall(tc.memory.Agent, tc.memory.Topic)
			if err != nil {
				t.Fatalf("Failed to recall: %v", err)
			}
			if content != tc.memory.Mind {
				t.Fatalf("Expected content: %v, got: %v", tc.memory.Mind, content)
			}
		})
	}
}

func TestChatHistory(t *testing.T) {
	// Create an in-memory SQLite database
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open SQLite in-memory database: %v", err)
	}
	defer db.Close()
	// Create the chat table
	_, err = db.Exec(`
		CREATE TABLE chats (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    name TEXT NOT NULL,
	    msgs TEXT NOT NULL, 
	    agent TEXT NOT NULL,
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

func TestVecTable(t *testing.T) {
	// healthcheck
	db, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	stmt, _, err := db.Prepare(`SELECT sqlite_version(), vec_version()`)
	if err != nil {
		t.Fatal(err)
	}
	stmt.Step()
	log.Printf("sqlite_version=%s, vec_version=%s\n", stmt.ColumnText(0), stmt.ColumnText(1))
	stmt.Close()
	// migration
	err = db.Exec("CREATE VIRTUAL TABLE vec_items USING vec0(embedding float[4], chat_name TEXT NOT NULL)")
	if err != nil {
		t.Fatal(err)
	}
	// data prep and insert
	items := map[int][]float32{
		1: {0.1, 0.1, 0.1, 0.1},
		2: {0.2, 0.2, 0.2, 0.2},
		3: {0.3, 0.3, 0.3, 0.3},
		4: {0.4, 0.4, 0.4, 0.4},
		5: {0.5, 0.5, 0.5, 0.5},
	}
	q := []float32{0.28, 0.3, 0.3, 0.3}
	stmt, _, err = db.Prepare("INSERT INTO vec_items(rowid, embedding, chat_name) VALUES (?, ?, ?)")
	if err != nil {
		t.Fatal(err)
	}
	for id, values := range items {
		v, err := sqlite_vec.SerializeFloat32(values)
		if err != nil {
			t.Fatal(err)
		}
		stmt.BindInt(1, id)
		stmt.BindBlob(2, v)
		stmt.BindText(3, "some_chat")
		err = stmt.Exec()
		if err != nil {
			t.Fatal(err)
		}
		stmt.Reset()
	}
	stmt.Close()
	// select | vec search
	stmt, _, err = db.Prepare(`
		SELECT
			rowid,
			distance,
			embedding
		FROM vec_items
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT 3
	`)
	if err != nil {
		t.Fatal(err)
	}
	query, err := sqlite_vec.SerializeFloat32(q)
	if err != nil {
		t.Fatal(err)
	}
	stmt.BindBlob(1, query)
	for stmt.Step() {
		rowid := stmt.ColumnInt64(0)
		distance := stmt.ColumnFloat(1)
		emb := stmt.ColumnRawText(2)
		floats := decodeUnsafe(emb)
		log.Printf("rowid=%d, distance=%f, floats=%v\n", rowid, distance, floats)
	}
	if err := stmt.Err(); err != nil {
		t.Fatal(err)
	}
	err = stmt.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = db.Close()
	if err != nil {
		t.Fatal(err)
	}
}
