# RAG Implementation Issues and Proposed Solutions

## Overview
The current RAG system had several limitations preventing reliable retrieval across multiple documents. Initial tests showed failures for queries like "two she bears" (KJV Bible 2 Kings 2:23-24), where the system would retrieve documents containing "bear" but miss the specific verse due to issues with chunking, query processing, and retrieval.

**Recent improvements** have addressed several key issues through targeted enhancements:

1. **Chunk overlap**: Added configurable overlap (`RAGOverlapWords`) to preserve context across chunk boundaries
2. **Hybrid retrieval**: Implemented FTS5 BM25 keyword search combined with embedding similarity via Reciprocal Rank Fusion (RRF)
3. **Query refinement**: Enhanced stopword preservation for short queries and filename contamination filtering
4. **Cross-document diversity**: Added per-file result caps to ensure multiple documents are represented
5. **Robust FTS5 queries**: Added OR fallback for multi-term queries when AND logic fails

**Result**: The system now successfully performs cross-document search, as demonstrated in `chat_exports/54_assistant.json` where queries like "Krahi Andrihee level" retrieve relevant information from both `ghost_7.txt` and `Overlord Volume 01 - The Undead King.epub`, handling LLM-injected terms like "Overlord" gracefully.

Below we dissect each original problem, note implementation status, and describe the actual solutions deployed.

---

### Problem 1: Chunking Destroys Semantic Coherence [PARTIALLY ADDRESSED]
- **Problem description**  
  The current chunking splits text into sentences and groups them by a simple word count threshold (`RAGWordLimit`). This ignores document structure (chapters, headings) and can cut through narrative units, scattering related content across multiple chunks. For the Bible query, the story of Elisha and the bears likely spans multiple verses; splitting it prevents any single chunk from containing the full context, diluting the embedding signal and making retrieval difficult.

- **Implemented solution**  
  - **Overlap between chunks**: Added `RAGOverlapWords` configuration (default 16 words) to `createChunks()` function, ensuring continuity across chunk boundaries. This preserves context for phrases that might be split, though full structure-aware chunking remains future work.
  - The overlap mechanism calculates word-level overlap, skipping sentences as needed to achieve the configured overlap size while maintaining chunk size limits.

- **Proposed solution (remaining)**  
  - **Structure-aware chunking**: Use the EPUB’s internal structure (chapters, sections) to create chunks that align with logical content units (e.g., by chapter or story).  
  - **Rich metadata**: Store book name, chapter, and verse numbers with each chunk to enable filtering and source attribution.  
  - **Fallback to recursive splitting**: For documents without clear structure, use a recursive character text splitter with overlap (similar to LangChain’s `RecursiveCharacterTextSplitter`) to maintain semantic boundaries (paragraphs, sentences).

---

### Problem 2: Query Refinement Strips Critical Context [PARTIALLY ADDRESSED]
- **Problem description**  
  `RefineQuery` removes stop words and applies keyword-based filtering that discards semantically important modifiers. For "two she bears", the word "she" (a gender modifier) may be treated as a stop word, leaving "two bears". This loses the specificity of the query and causes the embedding to drift toward generic "bear" contexts. The rule-based approach cannot understand that "she bears" is a key phrase in the biblical story.

- **Implemented solution**  
  - **Stopword preservation for short queries**: Modified `RefineQuery()` to skip stopword removal entirely for queries with fewer than 3 words. This preserves critical modifiers like "she" in "two she bears" while still cleaning longer, noisier queries.
  - **Filename contamination filtering**: Extended `GenerateQueryVariations()` to filter out query terms that match loaded filenames (case-insensitive). This prevents LLM-injected terms like "Overlord" from contaminating queries when searching across documents.
  - **Query variation generation**: Maintained existing variation generation (prefix/suffix trimming, adding "explanation", "details", etc.) to improve embedding alignment.

- **Proposed solution (remaining)**  
  - **Entity-aware query preservation**: Use a lightweight NLP model (e.g., spaCy or a BERT-based NER tagger) to identify and retain key entities (quantities, animals, names) while only removing truly irrelevant stop words.  
  - **Intelligent query rewriting**: Employ a small LLM (or a set of transformation rules) to generate query variations that reflect likely biblical phrasing, e.g., "two bears came out of the wood" or "Elisha and the bears".  
  - **Contextual stop word removal**: Instead of a static list, use a POS tagger to keep adjectives, nouns, and verbs while removing only function words that don't carry meaning.

---

### Problem 3: Embedding Similarity Fails for Rare or Specific Phrases [ADDRESSED]
- **Problem description**  
  Dense embeddings excel at capturing semantic similarity but can fail when the query contains rare phrases or when the relevant passage is embedded in a noisy chunk. The verse "there came forth two she bears out of the wood" shares only the word "bears" with the query, and its embedding may be pulled toward the average of surrounding verses. Consequently, the similarity score may be lower than that of other chunks containing the word "bear" in generic contexts.

- **Implemented solution**  
  - **Hybrid retrieval (FTS5 + embeddings)**: 
    - Created FTS5 virtual table (`fts_embeddings`) with porter stemmer tokenizer
    - Implemented `SearchKeyword()` using BM25 ranking with proper score-to-distance conversion
    - Combined embedding and keyword results using **Reciprocal Rank Fusion (RRF)** with k=60
    - Results from both methods are deduplicated and scored jointly
  - **Query variation expansion**: Enhanced `GenerateQueryVariations()` to produce multiple query forms (trimmed prefixes/suffixes, added "explanation"/"details"/"summary")
  - **FTS5 OR fallback**: Modified `SearchKeyword()` to automatically retry with OR operator when AND query returns zero results, handling LLM-injected terms gracefully
  - **Increased retrieval breadth**: Modified `SearchClosest()` to retrieve more candidates (limit × 2) before RRF fusion

- **Proposed solution (remaining)**  
  - **Fine-tuned embeddings**: Consider using an embedding model fine-tuned on domain-specific data (e.g., biblical texts) if this is a recurring use case.  

---

### Problem 4: Reranking Heuristics Are Insufficient [PARTIALLY ADDRESSED]
- **Problem description**  
  `RerankResults` boosts results based on simple keyword matching and file name heuristics. This coarse approach cannot reliably promote the correct verse over false positives. The adjustment `distance - score/100` is arbitrary and may not reflect true relevance.

- **Implemented solution**  
  - **Document diversity cap**: Modified `RerankResults()` to limit results to **maximum 2 per file**. This ensures cross-document representation and prevents single-document dominance in results.
  - **Enhanced scoring**: Maintained existing keyword match scoring (exact query match +10, partial word matches proportional) but added per-file tracking to enforce diversity.
  - **Result limiting**: Final results capped at 10 unique chunks after deduplication and file diversity enforcement.

- **Proposed solution (remaining)**  
  - **Cross-encoder reranking**: After retrieving top candidates (e.g., top 20) with hybrid search, rerank them using a cross-encoder model that directly computes the relevance score between the query and each chunk.  
    - Models like `cross-encoder/ms-marco-MiniLM-L-6-v2` are lightweight and can be run locally or via a microservice.  
  - **Score normalization**: Use the cross-encoder scores to reorder results, discarding low-scoring ones.  
  - **Contextual boosting**: If metadata (e.g., chapter/verse) is available, boost results that match the query’s expected location (if inferable).  

---

### Problem 5: Answer Synthesis Is Not Generative [NOT ADDRESSED]
- **Problem description**  
  `SynthesizeAnswer` embeds a prompt and attempts to retrieve a pre-stored answer, falling back to concatenating truncated chunks. This is fundamentally flawed: RAG requires an LLM to generate a coherent answer from retrieved context. In the Bible example, even if the correct verse were retrieved, the system would only output a snippet, not an answer explaining the reference.

- **Proposed solution**  
  - **Integrate an LLM for generation**: Use a local model (via Ollama, Llama.cpp) or a cloud API (OpenAI, etc.) to synthesize answers.  
    - Construct a prompt that includes the retrieved chunks (with metadata) and the user query.  
    - Instruct the model to answer based solely on the provided context and cite sources (e.g., "According to 2 Kings 2:24...").  
  - **Implement a fallback**: If no relevant chunks are retrieved, return a message like "I couldn't find that information in your documents."  
  - **Streaming support**: For better UX, stream the answer token-by-token.  

---

### Problem 6: Concurrency and Error Handling [NOT ADDRESSED]
- **Problem description**  
  The code uses a mutex only in `LoadRAG`, leaving other methods vulnerable to race conditions. The global status channel `LongJobStatusCh` may drop messages due to `select/default`, and errors are sometimes logged but not propagated. Ingestion is synchronous and slow.

- **Proposed solution**  
  - **Add context support**: Pass `context.Context` to all methods to allow cancellation and timeouts.  
  - **Worker pools for embedding**: Parallelize batch embedding with a controlled number of workers to respect API rate limits and speed up ingestion.  
  - **Retry logic**: Implement exponential backoff for transient API errors.  
  - **Replace global channel**: Use a callback or an injectable status reporter to avoid dropping messages.  
  - **Fine-grained locking**: Protect shared state (e.g., `storage`) with appropriate synchronization.  

---

### Problem 7: Lack of Monitoring and Evaluation [NOT ADDRESSED]
- **Problem description**  
  There are no metrics to track retrieval quality, latency, or user satisfaction. The failure case was discovered manually; without systematic evaluation, regressions will go unnoticed.

- **Proposed solution**  
  - **Log key metrics**: Record query, retrieved chunk IDs, scores, and latency for each search.  
  - **User feedback**: Add a mechanism for users to rate answers (thumbs up/down) and use this data to improve retrieval.  
  - **Offline evaluation**: Create a test set of queries and expected relevant chunks (e.g., the Bible example) to measure recall@k, MRR, etc., and run it after each change.  

---

## Summary & Current Status
The original RAG improvement plan outlined 7 key areas. Recent implementation has partially or fully addressed several critical issues, particularly enabling successful cross-document search:

### Implemented / Partially Addressed
1. **✅ Hybrid retrieval (dense + sparse)** - FTS5 BM25 with RRF fusion, OR fallback for multi-term queries
2. **🔄 Structure-aware chunking** - Overlap between chunks (`RAGOverlapWords`), though full structure-awareness pending
3. **🔄 Query understanding** - Stopword preservation for short queries, filename contamination filtering
4. **🔄 Cross-document diversity** - Per-file result caps (max 2 per document) in reranking

### Key Achievements
- **Cross-document search**: Queries like "Krahi Andrihee level" now retrieve relevant information from multiple loaded documents
- **Robust query handling**: OR fallback handles LLM-injected terms (e.g., "Overlord" added to queries)
- **Document diversity**: Prevents single-document dominance in results
- **Context preservation**: Chunk overlap maintains semantic continuity

### Remaining Work
5. **LLM-based answer generation** - Still uses concatenation rather than generative synthesis
6. **Robust concurrency and error handling** - Race conditions, synchronous ingestion, status channel issues
7. **Monitoring and evaluation** - No metrics, logging, or systematic testing

### Next Steps
- Evaluate the improved system with the original "two she bears" test case
- Consider adding cross-encoder reranking for higher precision
- Address synthesis and concurrency issues as needed

The system has evolved from a brittle keyword matcher toward a more reliable knowledge assistant, though significant improvements remain for production-grade robustness.
