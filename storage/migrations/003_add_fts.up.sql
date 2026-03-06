-- Create FTS5 virtual table for full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS fts_embeddings USING fts5(
    slug UNINDEXED,
    raw_text,
    filename UNINDEXED,
    embedding_size UNINDEXED,
    tokenize='porter unicode61'  -- Use porter stemmer and unicode61 tokenizer
);

-- Create triggers to maintain FTS table when embeddings are inserted/deleted
-- Note: We'll handle inserts/deletes programmatically for simplicity
-- but triggers could be added here if needed.

-- Indexes for performance (FTS5 manages its own indexes)
-- No additional indexes needed for FTS5 virtual table.