CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
    embedding FLOAT[5120],
    slug TEXT NOT NULL,
    raw_text TEXT PRIMARY KEY,
);

CREATE VIRTUAL TABLE IF NOT EXISTS embeddings_384 USING vec0(
    embedding FLOAT[384],
    slug TEXT NOT NULL,
    raw_text TEXT PRIMARY KEY
);
