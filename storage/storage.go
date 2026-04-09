package storage

import (
	"database/sql"
	"gf-lt/models"
	"log/slog"

	_ "github.com/glebarez/go-sqlite"
	"github.com/jmoiron/sqlx"
)

type FullRepo interface {
	ChatHistory
	Memories
	VectorRepo
	TableLister
}

type TableLister interface {
	ListTables() ([]string, error)
	GetTableColumns(table string) ([]TableColumn, error)
}

type ChatHistory interface {
	ListChats() ([]models.Chat, error)
	GetChatByID(id uint32) (*models.Chat, error)
	GetChatByChar(char string) ([]models.Chat, error)
	GetLastChat() (*models.Chat, error)
	GetLastChatByAgent(agent string) (*models.Chat, error)
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

func (p ProviderSQL) GetChatByChar(char string) ([]models.Chat, error) {
	resp := []models.Chat{}
	err := p.db.Select(&resp, "SELECT * FROM chats WHERE agent=$1;", char)
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

func (p ProviderSQL) GetLastChatByAgent(agent string) (*models.Chat, error) {
	resp := models.Chat{}
	query := "SELECT * FROM chats WHERE agent=$1 ORDER BY updated_at DESC LIMIT 1"
	err := p.db.Get(&resp, query, agent)
	return &resp, err
}

// https://sqlite.org/lang_upsert.html
// on conflict was added
func (p ProviderSQL) UpsertChat(chat *models.Chat) (*models.Chat, error) {
	// Prepare the SQL statement
	query := `
        INSERT INTO chats (id, name, msgs, agent, created_at, updated_at)
	VALUES (:id, :name, :msgs, :agent, :created_at, :updated_at)
	ON CONFLICT(id) DO UPDATE SET msgs=excluded.msgs,
	updated_at=excluded.updated_at
        RETURNING *;`
	stmt, err := p.db.PrepareNamed(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
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

// opens database connection
func NewProviderSQL(dbPath string, logger *slog.Logger) FullRepo {
	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		logger.Error("failed to open db connection", "error", err)
		return nil
	}
	// Enable WAL mode for better concurrency and performance
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		logger.Warn("failed to enable WAL mode", "error", err)
	}
	if _, err := db.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		logger.Warn("failed to set synchronous mode", "error", err)
	}
	// Increase cache size for better performance
	if _, err := db.Exec("PRAGMA cache_size = -2000;"); err != nil {
		logger.Warn("failed to set cache size", "error", err)
	}
	// Log actual journal mode for debugging
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err == nil {
		logger.Debug("SQLite journal mode", "mode", journalMode)
	}
	p := ProviderSQL{db: db, logger: logger}
	if err := p.Migrate(); err != nil {
		logger.Error("migration failed, app cannot start", "error", err)
		return nil
	}
	return p
}

// DB returns the underlying database connection
func (p ProviderSQL) DB() *sqlx.DB {
	return p.db
}

type TableColumn struct {
	CID     int            `db:"cid"`
	Name    string         `db:"name"`
	Type    string         `db:"type"`
	NotNull bool           `db:"notnull"`
	DFltVal sql.NullString `db:"dflt_value"`
	PK      int            `db:"pk"`
}

func (p ProviderSQL) ListTables() ([]string, error) {
	resp := []string{}
	err := p.db.Select(&resp, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name;")
	return resp, err
}

func (p ProviderSQL) GetTableColumns(table string) ([]TableColumn, error) {
	resp := []TableColumn{}
	err := p.db.Select(&resp, "PRAGMA table_info("+table+");")
	return resp, err
}
