package storage

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
)

//go:embed migrations/*
var migrationsFS embed.FS

func (p *ProviderSQL) Migrate() {
	// Get the embedded filesystem
	migrationsDir, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		p.logger.Error("Failed to get embedded migrations directory;", "error", err)
	}
	// List all .up.sql files
	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		p.logger.Error("Failed to read migrations directory;", "error", err)
	}
	// Execute each .up.sql file
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".up.sql") {
			err := p.executeMigration(migrationsDir, file.Name())
			if err != nil {
				p.logger.Error("Failed to execute migration %s: %v", file.Name(), err)
				panic(err)
			}
		}
	}
	p.logger.Debug("All migrations executed successfully!")
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
	// Connect to the database (example using a simple connection)
	err := p.s3Conn.Exec(string(sqlContent))
	if err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}
	return nil
}
