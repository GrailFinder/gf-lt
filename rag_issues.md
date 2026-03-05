# RAG Implementation Issues and Proposed Solutions

## Overview
The current RAG system fails to retrieve relevant information for specific queries, as demonstrated by the inability to find the "two she bears" reference in the KJV Bible (2 Kings 2:23-24). While the system retrieves documents containing the word "bear", it misses the actual verse, indicating fundamental flaws in chunking, query processing, retrieval, and answer synthesis. Below we dissect each problem and propose concrete solutions.

---

### Problem 1: Chunking Destroys Semantic Coherence
- **Problem description**  
  The current chunking splits text into sentences and groups them by a simple word count threshold (`RAGWordLimit`). This ignores document structure (chapters, headings) and can cut through narrative units, scattering related content across multiple chunks. For the Bible query, the story of Elisha and the bears likely spans multiple verses; splitting it prevents any single chunk from containing the full context, diluting the embedding signal and making retrieval difficult.

- **Proposed solution**  
  - **Structure-aware chunking**: Use the EPUB’s internal structure (chapters, sections) to create chunks that align with logical content units (e.g., by chapter or story).  
  - **Overlap between chunks**: Add a configurable overlap (e.g., 10–20% of chunk size) to preserve continuity, ensuring key phrases like "two she bears" are not split across boundaries.  
  - **Rich metadata**: Store book name, chapter, and verse numbers with each chunk to enable filtering and source attribution.  
  - **Fallback to recursive splitting**: For documents without clear structure, use a recursive character text splitter with overlap (similar to LangChain’s `RecursiveCharacterTextSplitter`) to maintain semantic boundaries (paragraphs, sentences).

---

### Problem 2: Query Refinement Strips Critical Context
- **Problem description**  
  `RefineQuery` removes stop words and applies keyword-based filtering that discards semantically important modifiers. For "two she bears", the word "she" (a gender modifier) may be treated as a stop word, leaving "two bears". This loses the specificity of the query and causes the embedding to drift toward generic "bear" contexts. The rule-based approach cannot understand that "she bears" is a key phrase in the biblical story.

- **Proposed solution**  
  - **Entity-aware query preservation**: Use a lightweight NLP model (e.g., spaCy or a BERT-based NER tagger) to identify and retain key entities (quantities, animals, names) while only removing truly irrelevant stop words.  
  - **Intelligent query rewriting**: Employ a small LLM (or a set of transformation rules) to generate query variations that reflect likely biblical phrasing, e.g., "two bears came out of the wood" or "Elisha and the bears".  
  - **Contextual stop word removal**: Instead of a static list, use a POS tagger to keep adjectives, nouns, and verbs while removing only function words that don't carry meaning.  
  - **Disable refinement for short queries**: If the query is already concise (like "two she bears"), skip aggressive filtering.

---

### Problem 3: Embedding Similarity Fails for Rare or Specific Phrases
- **Problem description**  
  Dense embeddings excel at capturing semantic similarity but can fail when the query contains rare phrases or when the relevant passage is embedded in a noisy chunk. The verse "there came forth two she bears out of the wood" shares only the word "bears" with the query, and its embedding may be pulled toward the average of surrounding verses. Consequently, the similarity score may be lower than that of other chunks containing the word "bear" in generic contexts.

- **Proposed solution**  
  - **Hybrid retrieval**: Combine dense embeddings with BM25 (keyword) search. BM25 excels at exact phrase matching and would likely retrieve the verse based on "two bears" even if the embedding is weak.  
    - Use a library like [blevesearch](https://github.com/blevesearch/bleve) to index text alongside vectors.  
    - Fuse results using Reciprocal Rank Fusion (RRF) or a weighted combination.  
  - **Query expansion**: Add relevant terms to the query (e.g., "Elisha", "2 Kings") to improve embedding alignment.  
  - **Fine-tuned embeddings**: Consider using an embedding model fine-tuned on domain-specific data (e.g., biblical texts) if this is a recurring use case.  

---

### Problem 4: Reranking Heuristics Are Insufficient
- **Problem description**  
  `RerankResults` boosts results based on simple keyword matching and file name heuristics. This coarse approach cannot reliably promote the correct verse over false positives. The adjustment `distance - score/100` is arbitrary and may not reflect true relevance.

- **Proposed solution**  
  - **Cross-encoder reranking**: After retrieving top candidates (e.g., top 20) with hybrid search, rerank them using a cross-encoder model that directly computes the relevance score between the query and each chunk.  
    - Models like `cross-encoder/ms-marco-MiniLM-L-6-v2` are lightweight and can be run locally or via a microservice.  
  - **Score normalization**: Use the cross-encoder scores to reorder results, discarding low-scoring ones.  
  - **Contextual boosting**: If metadata (e.g., chapter/verse) is available, boost results that match the query’s expected location (if inferable).  

---

### Problem 5: Answer Synthesis Is Not Generative
- **Problem description**  
  `SynthesizeAnswer` embeds a prompt and attempts to retrieve a pre-stored answer, falling back to concatenating truncated chunks. This is fundamentally flawed: RAG requires an LLM to generate a coherent answer from retrieved context. In the Bible example, even if the correct verse were retrieved, the system would only output a snippet, not an answer explaining the reference.

- **Proposed solution**  
  - **Integrate an LLM for generation**: Use a local model (via Ollama, Llama.cpp) or a cloud API (OpenAI, etc.) to synthesize answers.  
    - Construct a prompt that includes the retrieved chunks (with metadata) and the user query.  
    - Instruct the model to answer based solely on the provided context and cite sources (e.g., "According to 2 Kings 2:24...").  
  - **Implement a fallback**: If no relevant chunks are retrieved, return a message like "I couldn't find that information in your documents."  
  - **Streaming support**: For better UX, stream the answer token-by-token.  

---

### Problem 6: Concurrency and Error Handling
- **Problem description**  
  The code uses a mutex only in `LoadRAG`, leaving other methods vulnerable to race conditions. The global status channel `LongJobStatusCh` may drop messages due to `select/default`, and errors are sometimes logged but not propagated. Ingestion is synchronous and slow.

- **Proposed solution**  
  - **Add context support**: Pass `context.Context` to all methods to allow cancellation and timeouts.  
  - **Worker pools for embedding**: Parallelize batch embedding with a controlled number of workers to respect API rate limits and speed up ingestion.  
  - **Retry logic**: Implement exponential backoff for transient API errors.  
  - **Replace global channel**: Use a callback or an injectable status reporter to avoid dropping messages.  
  - **Fine-grained locking**: Protect shared state (e.g., `storage`) with appropriate synchronization.  

---

### Problem 7: Lack of Monitoring and Evaluation
- **Problem description**  
  There are no metrics to track retrieval quality, latency, or user satisfaction. The failure case was discovered manually; without systematic evaluation, regressions will go unnoticed.

- **Proposed solution**  
  - **Log key metrics**: Record query, retrieved chunk IDs, scores, and latency for each search.  
  - **User feedback**: Add a mechanism for users to rate answers (thumbs up/down) and use this data to improve retrieval.  
  - **Offline evaluation**: Create a test set of queries and expected relevant chunks (e.g., the Bible example) to measure recall@k, MRR, etc., and run it after each change.  

---

## Summary
Fixing the RAG pipeline requires a multi-pronged approach:
1. **Structure-aware chunking** with metadata.
2. **Hybrid retrieval** (dense + sparse).
3. **Query understanding** via entity preservation and intelligent rewriting.
4. **Cross-encoder reranking** for precision.
5. **LLM-based answer generation**.
6. **Robust concurrency and error handling**.
7. **Monitoring and evaluation** to track improvements.

Implementing these changes will transform the system from a brittle keyword matcher into a reliable knowledge assistant capable of handling nuanced queries like the "two she bears" reference.
