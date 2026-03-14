-- Populate FTS table with existing embeddings (incremental - only inserts missing rows)
-- Only use 768 embeddings as that's what we use
INSERT OR IGNORE INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 768 FROM embeddings_768;