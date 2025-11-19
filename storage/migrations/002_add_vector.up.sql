-- Create tables for vector storage (replacing vec0 plugin usage)
CREATE TABLE IF NOT EXISTS embeddings_384 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings_5120 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for better performance
CREATE INDEX IF NOT EXISTS idx_embeddings_384_filename ON embeddings_384(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_5120_filename ON embeddings_5120(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_384_slug ON embeddings_384(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_5120_slug ON embeddings_5120(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_384_created_at ON embeddings_384(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_5120_created_at ON embeddings_5120(created_at);
