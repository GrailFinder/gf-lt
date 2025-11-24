-- Create tables for vector storage (replacing vec0 plugin usage)
CREATE TABLE IF NOT EXISTS embeddings_384 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings_768 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings_1024 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings_1536 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings_2048 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings_3072 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    embeddings BLOB NOT NULL,
    slug TEXT NOT NULL,
    raw_text TEXT NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings_4096 (
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
CREATE INDEX IF NOT EXISTS idx_embeddings_768_filename ON embeddings_768(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_1024_filename ON embeddings_1024(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_1536_filename ON embeddings_1536(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_2048_filename ON embeddings_2048(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_3072_filename ON embeddings_3072(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_4096_filename ON embeddings_4096(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_5120_filename ON embeddings_5120(filename);
CREATE INDEX IF NOT EXISTS idx_embeddings_384_slug ON embeddings_384(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_768_slug ON embeddings_768(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_1024_slug ON embeddings_1024(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_1536_slug ON embeddings_1536(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_2048_slug ON embeddings_2048(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_3072_slug ON embeddings_3072(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_4096_slug ON embeddings_4096(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_5120_slug ON embeddings_5120(slug);
CREATE INDEX IF NOT EXISTS idx_embeddings_384_created_at ON embeddings_384(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_768_created_at ON embeddings_768(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_1024_created_at ON embeddings_1024(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_1536_created_at ON embeddings_1536(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_2048_created_at ON embeddings_2048(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_3072_created_at ON embeddings_3072(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_4096_created_at ON embeddings_4096(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_5120_created_at ON embeddings_5120(created_at);
