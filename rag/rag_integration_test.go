package rag

import (
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"gf-lt/storage"
	"log/slog"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/jmoiron/sqlx"
)

// mockEmbedder returns zero vectors of a fixed dimension.
type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) Embed(text string) ([]float32, error) {
	vec := make([]float32, m.dim)
	return vec, nil
}

func (m *mockEmbedder) EmbedSlice(texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i := range vecs {
		vecs[i] = make([]float32, m.dim)
	}
	return vecs, nil
}

// dummyStore implements storage.FullRepo with a minimal set of methods.
// Only DB() is used by VectorStorage; other methods return empty values.
type dummyStore struct {
	db *sqlx.DB
}

func (d dummyStore) DB() *sqlx.DB { return d.db }

// ChatHistory methods
func (d dummyStore) ListChats() ([]models.Chat, error)                     { return nil, nil }
func (d dummyStore) GetChatByID(id uint32) (*models.Chat, error)           { return nil, nil }
func (d dummyStore) GetChatByChar(char string) ([]models.Chat, error)      { return nil, nil }
func (d dummyStore) GetLastChat() (*models.Chat, error)                    { return nil, nil }
func (d dummyStore) GetLastChatByAgent(agent string) (*models.Chat, error) { return nil, nil }
func (d dummyStore) UpsertChat(chat *models.Chat) (*models.Chat, error)    { return chat, nil }
func (d dummyStore) RemoveChat(id uint32) error                            { return nil }
func (d dummyStore) ChatGetMaxID() (uint32, error)                         { return 0, nil }

// Memories methods
func (d dummyStore) Memorise(m *models.Memory) (*models.Memory, error) { return m, nil }
func (d dummyStore) Recall(agent, topic string) (string, error)        { return "", nil }
func (d dummyStore) RecallTopics(agent string) ([]string, error)       { return nil, nil }

// VectorRepo methods (not used but required by interface)
func (d dummyStore) WriteVector(row *models.VectorRow) error { return nil }
func (d dummyStore) SearchClosest(q []float32, limit int) ([]models.VectorRow, error) {
	return nil, nil
}
func (d dummyStore) ListFiles() ([]string, error)              { return nil, nil }
func (d dummyStore) RemoveEmbByFileName(filename string) error { return nil }

var _ storage.FullRepo = dummyStore{}

// setupTestRAG creates an in‑memory SQLite database, creates the necessary tables,
// inserts the provided chunks, and returns a RAG instance with a mock embedder.
func setupTestRAG(t *testing.T, chunks []*models.VectorRow) (*RAG, error) {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open in‑memory db: %w", err)
	}
	// Create the required tables (embeddings_768 and fts_embeddings).
	// Use the same schema as production.
	_, err = db.Exec(`
		CREATE TABLE embeddings_768 (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			embeddings BLOB NOT NULL,
			slug TEXT NOT NULL,
			raw_text TEXT NOT NULL,
			filename TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("create embeddings table: %w", err)
	}
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE fts_embeddings USING fts5(
			slug UNINDEXED,
			raw_text,
			filename UNINDEXED,
			embedding_size UNINDEXED,
			tokenize='porter unicode61'
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("create FTS table: %w", err)
	}
	// Create a logger that discards output.
	logger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
	store := dummyStore{db: db}
	// Create config with embedding dimension 768.
	cfg := &config.Config{
		EmbedDims:       768,
		RAGWordLimit:    250,
		RAGOverlapWords: 25,
		RAGBatchSize:    1,
	}
	// Create a RAG instance using New, which will create an embedder based on config.
	// We'll override the embedder afterwards via reflection.
	rag, err := New(logger, store, cfg)
	if err != nil {
		return nil, fmt.Errorf("create RAG: %w", err)
	}
	// Replace the embedder with our mock.
	rag.SetEmbedderForTesting(&mockEmbedder{dim: cfg.EmbedDims})
	// Insert the provided chunks using the storage directly.
	if len(chunks) > 0 {
		// Ensure each chunk has embeddings of correct dimension (zero vector).
		for _, chunk := range chunks {
			if len(chunk.Embeddings) != cfg.EmbedDims {
				chunk.Embeddings = make([]float32, cfg.EmbedDims)
			}
		}
		err = rag.storage.WriteVectors(chunks)
		if err != nil {
			return nil, fmt.Errorf("write test chunks: %w", err)
		}
	}
	return rag, nil
}

// createTestChunks returns a slice of VectorRow representing the target chunk
// (kjv_bible.epub_1786_0), several bald‑related noise chunks, and unrelated chunks.
func createTestChunks() []*models.VectorRow {
	// Target chunk: 2 Kings 2:23‑24 containing "bald head" and "two she bears".
	targetRaw := `And he said, Ye shall not send. 


2:17 And when they urged him till he was ashamed, he said, Send.  They sent
therefore fifty men; and they sought three days, but found him not. 


2:18 And when they came again to him, (for he tarried at Jericho,) he said unto
them, Did I not say unto you, Go not?  2:19 And the men of the city said unto
Elisha, Behold, I pray thee, the situation of this city is pleasant, as my lord
seeth: but the water is naught, and the ground barren. 


2:20 And he said, Bring me a new cruse, and put salt therein.  And they brought
it to him. 


2:21 And he went forth unto the spring of the waters, and cast the salt in
there, and said, Thus saith the LORD, I have healed these waters; there shall
not be from thence any more death or barren land. 


2:22 So the waters were healed unto this day, according to the saying of Elisha
which he spake. 


2:23 And he went up from thence unto Bethel: and as he was going up by the way,
there came forth little children out of the city, and mocked him, and said unto
him, Go up, thou bald head; go up, thou bald head. 


2:24 And he turned back, and looked on them, and cursed them in the name of the
LORD.  And there came forth two she bears out of the wood, and tare forty and
two children of them.`
	// Noise chunk 1: Leviticus containing "bald locust"
	noise1Raw := `11:12 Whatsoever hath no fins nor scales in the waters, that shall be an
abomination unto you. 


11:13 And these are they which ye shall have in abomination among the fowls;
they shall not be eaten, they are an abomination: the eagle, and the ossifrage,
and the ospray, 11:14 And the vulture, and the kite after his kind; 11:15 Every
raven after his kind; 11:16 And the owl, and the night hawk, and the cuckow,
and the hawk after his kind, 11:17 And the little owl, and the cormorant, and
the great owl, 11:18 And the swan, and the pelican, and the gier eagle, 11:19
And the stork, the heron after her kind, and the lapwing, and the bat. 


11:20 All fowls that creep, going upon all four, shall be an abomination unto
you. 


11:21 Yet these may ye eat of every flying creeping thing that goeth upon all
four, which have legs above their feet, to leap withal upon the earth; 11:22
Even these of them ye may eat; the locust after his kind, and the bald locust
after his kind, and the beetle after his kind, and the grasshopper after his
kind. 


11:23 But all other flying creeping things, which have four feet, shall be an
abomination unto you. 


11:24 And for these ye shall be unclean: whosoever toucheth the carcase of them
shall be unclean until the even.`
	// Noise chunk 2: Leviticus containing "bald"
	noise2Raw := `11:13 And these are they which ye shall have in abomination among the fowls;
they shall not be eaten, they are an abomination: the eagle, and the ossifrage,
and the ospray, 11:14 And the vulture, and the kite after his kind; 11:15 Every
raven after his kind; 11:16 And the owl, and the night hawk, and the cuckow,
and the hawk after his kind, 11:17 And the little owl, and the cormorant, and
the great owl, 11:18 And the swan, and the pelican, and the gier eagle, 11:19
And the stork, the heron after her kind, and the lapwing, and the bat. 


11:20 All fowls that creep, going upon all four, shall be an abomination unto
you. 


11:21 Yet these may ye eat of every flying creeping thing that goeth upon all
four, which have legs above their feet, to leap withal upon the earth; 11:22
Even these of them ye may eat; the locust after his kind, and the bald locust
after his kind, and the beetle after his kind, and the grasshopper after his
kind. 


11:23 But all other flying creeping things, which have four feet, shall be an
abomination unto you. 


11:24 And for these ye shall be unclean: whosoever toucheth the carcase of them
shall be unclean until the even.`
	// Additional Leviticus noise chunks (simulating 28 bald-related chunks)
	// Using variations of the same text with different slugs
	leviticusSlugs := []string{
		"kjv_bible.epub_564_0",
		"kjv_bible.epub_565_0",
		"kjv_bible.epub_579_0",
		"kjv_bible.epub_580_0",
		"kjv_bible.epub_581_0",
		"kjv_bible.epub_582_0",
		"kjv_bible.epub_583_0",
		"kjv_bible.epub_584_0",
		"kjv_bible.epub_585_0",
		"kjv_bible.epub_586_0",
		"kjv_bible.epub_587_0",
		"kjv_bible.epub_588_0",
		"kjv_bible.epub_589_0",
		"kjv_bible.epub_590_0",
	}
	leviticusTexts := []string{
		noise1Raw,
		noise2Raw,
		`13:40 And the man whose hair is fallen off his head, he is bald; yet is he
clean. 


13:41 And he that hath his hair fallen off from the part of his head toward his
face, he is forehead bald; yet is he clean.`,
		`13:42 And if there be in the bald head, or bald forehead, a white reddish sore;
it is a leprosy sprung up in his bald head, or his bald forehead.`,
		`13:43 Then the priest shall look upon it: and, behold, if the rising of the
sore be white reddish in his bald head, or in his bald forehead, as the leprosy
appearedh in the skin of the flesh;`,
		`13:44 He is a leprous man, he is unclean: the priest shall pronounce him utterly
unclean; his plague is in his head.`,
		`13:45 And the leper in whom the plague is, his clothes shall be rent, and his
head bare, and he shall put a covering upon his upper lip, and shall cry,
Unclean, unclean.`,
		`13:46 All the days wherein the plague shall be in him he shall be defiled; he
is unclean: he shall dwell alone; without the camp shall his habitation be.`,
		`13:47 The garment also that the plague of leprosy is in, whether it be a woollen
garment, or a linen garment;`,
		`13:48 Whether it be in the warp, or woof; of linen, or of woollen; whether in a
skin, or in any thing made of skin;`,
		`13:49 And if the plague be greenish or reddish in the garment, or in the skin,
either in the warp, or in the woof, or in any thing of skin; it is a plague of
leprosy, and shall be shewed unto the priest:`,
		`13:50 And the priest shall look upon the plague, and shut up it that hath the
plague seven days:`,
		`13:51 And he shall look on the plague on the seventh day: if the plague be spread
in the garment, either in the warp, or in the woof, or in a skin, or in any work
that is made of skin; the plague is a fretting leprosy; it is unclean.`,
		`13:52 He shall therefore burn that garment, whether warp or woof, in woollen or
in linen, or any thing of skin, wherein the plague is: for it is a fretting
leprosy; it shall be burnt in the fire.`,
	}
	// Unrelated chunk 1: ghost_7.txt_777_0
	unrelated1Raw := `Doesn’t he have any pride as a hunter?!  

I didn’t see what other choice I had.  I would just have to grovel and be ready to flee at any given moment.  
The Hidden Curse clan house was in the central region of the imperial capital.  It was a high-class area with extraordinary property values that hosted the residences of people like Lord Gladis.  This district was near the Imperial Castle, though “near” was a 
relative term as it was still a few kilometers away.  

The clan house was made of brick and conformed to an older style of architecture.`
	// Unrelated chunk 2: ghost_7.txt_778_0
	unrelated2Raw := `I would just have to grovel and be ready to flee at any given moment.  
The Hidden Curse clan house was in the central region of the imperial capital.  It was a high-class area with extraordinary property values that hosted the residences of people like Lord Gladis.  This district was near the Imperial Castle, though “near” was a 
relative term as it was still a few kilometers away.  

The clan house was made of brick and conformed to an older style of architecture.  Nearly everyone knew about this mansion and its clock tower.  It stood tall over the neighboring mansions and rumor had it that you could see the whole capital from the top.  It 
spoke to this clan’s renown and history that they were able to get away with building something that dwarfed the mansions of the nobility.`
	chunks := []*models.VectorRow{
		{
			Slug:       "kjv_bible.epub_1786_0",
			RawText:    targetRaw,
			FileName:   "kjv_bible.epub",
			Embeddings: nil, // will be filled with zero vector later
		},
	}
	// Add Leviticus noise chunks
	for i, slug := range leviticusSlugs {
		text := leviticusTexts[i%len(leviticusTexts)]
		chunks = append(chunks, &models.VectorRow{
			Slug:       slug,
			RawText:    text,
			FileName:   "kjv_bible.epub",
			Embeddings: nil,
		})
	}
	// Add unrelated chunks
	chunks = append(chunks,
		&models.VectorRow{
			Slug:       "ghost_7.txt_777_0",
			RawText:    unrelated1Raw,
			FileName:   "ghost_7.txt",
			Embeddings: nil,
		},
		&models.VectorRow{
			Slug:       "ghost_7.txt_778_0",
			RawText:    unrelated2Raw,
			FileName:   "ghost_7.txt",
			Embeddings: nil,
		},
	)
	return chunks
}
func assertTargetInTopN(t *testing.T, results []models.VectorRow, topN int) bool {
	t.Helper()
	for i, row := range results {
		if i >= topN {
			break
		}
		if row.Slug == "kjv_bible.epub_1786_0" {
			return true
		}
	}
	return false
}

func TestBiblicalQuery(t *testing.T) {
	chunks := createTestChunks()
	rag, err := setupTestRAG(t, chunks)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	query := "bald prophet and two she bears"
	results, err := rag.Search(query, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	// The target chunk should be in the top results.
	if !assertTargetInTopN(t, results, 5) {
		t.Errorf("target chunk not found in top 5 results for query %q", query)
		t.Logf("results slugs: %v", func() []string {
			slugs := make([]string, len(results))
			for i, r := range results {
				slugs[i] = r.Slug
			}
			return slugs
		}())
	}
}

func TestQueryVariations(t *testing.T) {
	chunks := createTestChunks()
	rag, err := setupTestRAG(t, chunks)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	tests := []struct {
		name  string
		query string
		topN  int
	}{
		{"she bears", "she bears", 5},
		{"bald head", "bald head", 5},
		{"two she bears out of the wood", "two she bears out of the wood", 5},
		{"bald prophet", "bald prophet", 10},
		{"go up thou bald head", "\"go up thou bald head\"", 5},
		{"two she bears", "\"two she bears\"", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := rag.Search(tt.query, 10)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if !assertTargetInTopN(t, results, tt.topN) {
				t.Errorf("target chunk not found in top %d results for query %q", tt.topN, tt.query)
				t.Logf("results slugs: %v", func() []string {
					slugs := make([]string, len(results))
					for i, r := range results {
						slugs[i] = r.Slug
					}
					return slugs
				}())
			}
		})
	}
}
