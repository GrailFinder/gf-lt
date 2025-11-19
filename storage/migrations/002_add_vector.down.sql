-- Drop vector storage tables
DROP INDEX IF EXISTS idx_embeddings_384_filename;
DROP INDEX IF EXISTS idx_embeddings_5120_filename;
DROP INDEX IF EXISTS idx_embeddings_384_slug;
DROP INDEX IF EXISTS idx_embeddings_5120_slug;
DROP INDEX IF EXISTS idx_embeddings_384_created_at;
DROP INDEX IF EXISTS idx_embeddings_5120_created_at;

DROP TABLE IF EXISTS embeddings_384;
DROP TABLE IF EXISTS embeddings_5120;