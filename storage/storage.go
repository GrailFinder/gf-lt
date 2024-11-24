package storage

import (
	"elefant/models"
	"log/slog"

	_ "github.com/glebarez/go-sqlite"
	"github.com/jmoiron/sqlx"
)

type FullRepo interface {
	ChatHistory
	Memories
}

type ChatHistory interface {
	ListChats() ([]models.Chat, error)
	GetChatByID(id uint32) (*models.Chat, error)
	GetLastChat() (*models.Chat, error)
	UpsertChat(chat *models.Chat) (*models.Chat, error)
	RemoveChat(id uint32) error
	ChatGetMaxID() (uint32, error)
}

type ProviderSQL struct {
	db     *sqlx.DB
	logger *slog.Logger
}

func (p ProviderSQL) ListChats() ([]models.Chat, error) {
	resp := []models.Chat{}
	err := p.db.Select(&resp, "SELECT * FROM chats;")
	return resp, err
}

func (p ProviderSQL) GetChatByID(id uint32) (*models.Chat, error) {
	resp := models.Chat{}
	err := p.db.Get(&resp, "SELECT * FROM chats WHERE id=$1;", id)
	return &resp, err
}

func (p ProviderSQL) GetLastChat() (*models.Chat, error) {
	resp := models.Chat{}
	err := p.db.Get(&resp, "SELECT * FROM chats ORDER BY updated_at DESC LIMIT 1")
	return &resp, err
}

func (p ProviderSQL) UpsertChat(chat *models.Chat) (*models.Chat, error) {
	// Prepare the SQL statement
	query := `
        INSERT OR REPLACE INTO chats (id, name, msgs, created_at, updated_at)
        VALUES (:id, :name, :msgs, :created_at, :updated_at)
        RETURNING *;`
	stmt, err := p.db.PrepareNamed(query)
	if err != nil {
		return nil, err
	}
	// Execute the query and scan the result into a new chat object
	var resp models.Chat
	err = stmt.Get(&resp, chat)
	return &resp, err
}

func (p ProviderSQL) RemoveChat(id uint32) error {
	query := "DELETE FROM chats WHERE ID = $1;"
	_, err := p.db.Exec(query, id)
	return err
}

func (p ProviderSQL) ChatGetMaxID() (uint32, error) {
	query := "SELECT MAX(id) FROM chats;"
	var id uint32
	err := p.db.Get(&id, query)
	return id, err
}

func NewProviderSQL(dbPath string, logger *slog.Logger) FullRepo {
	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		logger.Error("failed to open db connection", "error", err)
		return nil
	}
	p := ProviderSQL{db: db, logger: logger}
	p.Migrate()
	return p
}
