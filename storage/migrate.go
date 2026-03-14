package storage

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed migrations/*
var migrationsFS embed.FS

func (p *ProviderSQL) Migrate() error {
	// Get the embedded filesystem
	migrationsDir, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		p.logger.Error("Failed to get embedded migrations directory;", "error", err)
		return fmt.Errorf("failed to get embedded migrations directory: %w", err)
	}
	// List all .up.sql files
	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		p.logger.Error("Failed to read migrations directory;", "error", err)
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Check if FTS already has data - skip populate migration if so
	var ftsCount int
	_ = p.db.QueryRow("SELECT COUNT(*) FROM fts_embeddings").Scan(&ftsCount)
	skipFTSMigration := ftsCount > 0

	// Execute each .up.sql file
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".up.sql") {
			// Skip FTS populate migration if already populated
			if skipFTSMigration && strings.Contains(file.Name(), "004_populate_fts") {
				p.logger.Debug("Skipping FTS migration - already populated", "file", file.Name())
				continue
			}
			err := p.executeMigration(migrationsDir, file.Name())
			if err != nil {
				p.logger.Error("Failed to execute migration %s: %v", file.Name(), err)
				return fmt.Errorf("failed to execute migration %s: %w", file.Name(), err)
			}
		}
	}
	p.logger.Debug("All migrations executed successfully!")
	return nil
}

func (p *ProviderSQL) executeMigration(migrationsDir fs.FS, fileName string) error {
	// Open the migration file
	migrationFile, err := migrationsDir.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open migration file %s: %w", fileName, err)
	}
	defer migrationFile.Close()
	// Read the migration file content
	migrationContent, err := fs.ReadFile(migrationsDir, fileName)
	if err != nil {
		return fmt.Errorf("failed to read migration file %s: %w", fileName, err)
	}
	// Execute the migration content
	return p.executeSQL(migrationContent)
}

func (p *ProviderSQL) executeSQL(sqlContent []byte) error {
	// Execute the migration content using standard database connection
	_, err := p.db.Exec(string(sqlContent))
	if err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}
	return nil
}
