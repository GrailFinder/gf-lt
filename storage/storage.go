package storage

import (
	"elefant/models"
	"log/slog"

	_ "github.com/glebarez/go-sqlite"
	"github.com/jmoiron/sqlx"
)

type ChatHistory interface {
	ListChats() ([]models.Chat, error)
	GetChatByID(id uint32) (*models.Chat, error)
	GetLastChat() (*models.Chat, error)
	UpsertChat(chat *models.Chat) (*models.Chat, error)
	RemoveChat(id uint32) error
}

type ProviderSQL struct {
	db     *sqlx.DB
	logger *slog.Logger
}

func (p ProviderSQL) ListChats() ([]models.Chat, error) {
	resp := []models.Chat{}
	err := p.db.Select(&resp, "SELECT * FROM chat;")
	return resp, err
}

func (p ProviderSQL) GetChatByID(id uint32) (*models.Chat, error) {
	resp := models.Chat{}
	err := p.db.Get(&resp, "SELECT * FROM chat WHERE id=$1;", id)
	return &resp, err
}

func (p ProviderSQL) GetLastChat() (*models.Chat, error) {
	resp := models.Chat{}
	err := p.db.Get(&resp, "SELECT * FROM chat ORDER BY updated_at DESC LIMIT 1")
	return &resp, err
}

func (p ProviderSQL) UpsertChat(chat *models.Chat) (*models.Chat, error) {
	// Prepare the SQL statement
	query := `
        INSERT OR REPLACE INTO chat (id, name, msgs, created_at, updated_at)
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
	query := "DELETE FROM chat WHERE ID = $1;"
	_, err := p.db.Exec(query, id)
	return err
}

func NewProviderSQL(dbPath string, logger *slog.Logger) ChatHistory {
	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		panic(err)
	}
	// get SQLite version
	p := ProviderSQL{db: db, logger: logger}
	p.Migrate()
	return p
}
