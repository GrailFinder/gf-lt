-- Populate FTS table with existing embeddings
DELETE FROM fts_embeddings;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 384 FROM embeddings_384;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 768 FROM embeddings_768;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 1024 FROM embeddings_1024;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 1536 FROM embeddings_1536;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 2048 FROM embeddings_2048;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 3072 FROM embeddings_3072;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 4096 FROM embeddings_4096;

INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size)
SELECT slug, raw_text, filename, 5120 FROM embeddings_5120;