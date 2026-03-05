package rag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	"github.com/yalue/onnxruntime_go"
)

// Embedder defines the interface for embedding text
type Embedder interface {
	Embed(text string) ([]float32, error)
	EmbedSlice(lines []string) ([][]float32, error)
}

// APIEmbedder implements embedder using an API (like Hugging Face, OpenAI, etc.)
type APIEmbedder struct {
	logger *slog.Logger
	client *http.Client
	cfg    *config.Config
}

func NewAPIEmbedder(l *slog.Logger, cfg *config.Config) *APIEmbedder {
	return &APIEmbedder{
		logger: l,
		client: &http.Client{},
		cfg:    cfg,
	}
}

func (a *APIEmbedder) Embed(text string) ([]float32, error) {
	payload, err := json.Marshal(
		map[string]any{"input": text, "encoding_format": "float"},
	)
	if err != nil {
		a.logger.Error("failed to marshal payload", "err", err.Error())
		return nil, err
	}
	req, err := http.NewRequest("POST", a.cfg.EmbedURL, bytes.NewReader(payload))
	if err != nil {
		a.logger.Error("failed to create new req", "err", err.Error())
		return nil, err
	}
	if a.cfg.HFToken != "" {
		req.Header.Add("Authorization", "Bearer "+a.cfg.HFToken)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.logger.Error("failed to embed text", "err", err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = fmt.Errorf("non 200 response; code: %v", resp.StatusCode)
		a.logger.Error(err.Error())
		return nil, err
	}
	embResp := &models.LCPEmbedResp{}
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		a.logger.Error("failed to decode embedding response", "err", err.Error())
		return nil, err
	}
	if len(embResp.Data) == 0 || len(embResp.Data[0].Embedding) == 0 {
		err = errors.New("empty embedding response")
		a.logger.Error("empty embedding response")
		return nil, err
	}
	return embResp.Data[0].Embedding, nil
}

func (a *APIEmbedder) EmbedSlice(lines []string) ([][]float32, error) {
	payload, err := json.Marshal(
		map[string]any{"input": lines, "encoding_format": "float"},
	)
	if err != nil {
		a.logger.Error("failed to marshal payload", "err", err.Error())
		return nil, err
	}
	req, err := http.NewRequest("POST", a.cfg.EmbedURL, bytes.NewReader(payload))
	if err != nil {
		a.logger.Error("failed to create new req", "err", err.Error())
		return nil, err
	}
	if a.cfg.HFToken != "" {
		req.Header.Add("Authorization", "Bearer "+a.cfg.HFToken)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.logger.Error("failed to embed text", "err", err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = fmt.Errorf("non 200 response; code: %v", resp.StatusCode)
		a.logger.Error(err.Error())
		return nil, err
	}
	embResp := &models.LCPEmbedResp{}
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		a.logger.Error("failed to decode embedding response", "err", err.Error())
		return nil, err
	}
	if len(embResp.Data) == 0 {
		err = errors.New("empty embedding response")
		a.logger.Error("empty embedding response")
		return nil, err
	}

	// Collect all embeddings from the response
	embeddings := make([][]float32, len(embResp.Data))
	for i := range embResp.Data {
		if len(embResp.Data[i].Embedding) == 0 {
			err = fmt.Errorf("empty embedding at index %d", i)
			a.logger.Error("empty embedding", "index", i)
			return nil, err
		}
		embeddings[i] = embResp.Data[i].Embedding
	}

	// Sort embeddings by index to match the order of input lines
	// API responses may not be in order
	for _, data := range embResp.Data {
		if data.Index >= len(embeddings) || data.Index < 0 {
			err = fmt.Errorf("invalid embedding index %d", data.Index)
			a.logger.Error("invalid embedding index", "index", data.Index)
			return nil, err
		}
		embeddings[data.Index] = data.Embedding
	}
	return embeddings, nil
}

// 1. Loading ONNX models locally
// 2. Using a Go ONNX runtime (like gorgonia/onnx or similar)
// 3. Converting text to embeddings without external API calls
type ONNXEmbedder struct {
	session       *onnxruntime_go.DynamicAdvancedSession
	tokenizer     *tokenizer.Tokenizer
	tokenizerPath string
	dims          int
	logger        *slog.Logger
	mu            sync.Mutex
	modelPath     string
}

var onnxInitOnce sync.Once
var onnxReady bool
var onnxLibPath string

var onnxLibPaths = []string{
	"/usr/lib/libonnxruntime.so",
	"/usr/local/lib/libonnxruntime.so",
	"/usr/lib/x86_64-linux-gnu/libonnxruntime.so",
	"/opt/onnxruntime/lib/libonnxruntime.so",
}

func findONNXLibrary() string {
	for _, path := range onnxLibPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func NewONNXEmbedder(modelPath, tokenizerPath string, dims int, logger *slog.Logger) (*ONNXEmbedder, error) {
	// Check if model and tokenizer files exist
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("ONNX model not found: %w", err)
	}
	if _, err := os.Stat(tokenizerPath); err != nil {
		return nil, fmt.Errorf("tokenizer not found: %w", err)
	}

	// Find ONNX library
	onnxLibPath = findONNXLibrary()
	if onnxLibPath == "" {
		return nil, errors.New("ONNX runtime library not found in standard locations")
	}

	emb := &ONNXEmbedder{
		tokenizerPath: tokenizerPath,
		dims:          dims,
		logger:        logger,
		modelPath:     modelPath,
	}
	return emb, nil
}

func (e *ONNXEmbedder) ensureInitialized() error {
	if e.session != nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != nil {
		return nil
	}

	// Load tokenizer lazily
	if e.tokenizer == nil {
		tok, err := pretrained.FromFile(e.tokenizerPath)
		if err != nil {
			return fmt.Errorf("failed to load tokenizer: %w", err)
		}
		e.tokenizer = tok
	}

	onnxInitOnce.Do(func() {
		onnxruntime_go.SetSharedLibraryPath(onnxLibPath)
		if err := onnxruntime_go.InitializeEnvironment(); err != nil {
			e.logger.Error("failed to initialize ONNX runtime", "error", err)
			onnxReady = false
			return
		}
		onnxReady = true
	})
	if !onnxReady {
		return errors.New("ONNX runtime not ready")
	}
	session, err := onnxruntime_go.NewDynamicAdvancedSession(
		e.getModelPath(),
		[]string{"input_ids", "attention_mask"},
		[]string{"sentence_embedding"},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create ONNX session: %w", err)
	}
	e.session = session
	return nil
}

func (e *ONNXEmbedder) getModelPath() string {
	return e.modelPath
}

func (e *ONNXEmbedder) Embed(text string) ([]float32, error) {
	if err := e.ensureInitialized(); err != nil {
		return nil, err
	}
	// 1. Tokenize
	encoding, err := e.tokenizer.EncodeSingle(text)
	if err != nil {
		return nil, fmt.Errorf("tokenization failed: %w", err)
	}
	// 2. Convert to int64 and create attention mask
	ids := encoding.Ids
	inputIDs := make([]int64, len(ids))
	attentionMask := make([]int64, len(ids))
	for i, id := range ids {
		inputIDs[i] = int64(id)
		attentionMask[i] = 1
	}
	// 3. Create input tensors (shape: [1, seq_len])
	seqLen := int64(len(inputIDs))
	inputIDsTensor, err := onnxruntime_go.NewTensor[int64](
		onnxruntime_go.NewShape(1, seqLen),
		inputIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create input_ids tensor: %w", err)
	}
	defer func() { _ = inputIDsTensor.Destroy() }()
	maskTensor, err := onnxruntime_go.NewTensor[int64](
		onnxruntime_go.NewShape(1, seqLen),
		attentionMask,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create attention_mask tensor: %w", err)
	}
	defer func() { _ = maskTensor.Destroy() }()
	// 4. Create output tensor
	outputTensor, err := onnxruntime_go.NewEmptyTensor[float32](
		onnxruntime_go.NewShape(1, int64(e.dims)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create output tensor: %w", err)
	}
	defer func() { _ = outputTensor.Destroy() }()
	// 5. Run inference
	err = e.session.Run(
		[]onnxruntime_go.Value{inputIDsTensor, maskTensor},
		[]onnxruntime_go.Value{outputTensor},
	)
	if err != nil {
		return nil, fmt.Errorf("inference failed: %w", err)
	}
	// 6. Copy output data
	outputData := outputTensor.GetData()
	embedding := make([]float32, len(outputData))
	copy(embedding, outputData)
	return embedding, nil
}

func (e *ONNXEmbedder) EmbedSlice(texts []string) ([][]float32, error) {
	encodings := make([]*tokenizer.Encoding, len(texts))
	maxLen := 0
	for i, txt := range texts {
		enc, err := e.tokenizer.EncodeSingle(txt)
		if err != nil {
			return nil, err
		}
		encodings[i] = enc
		if l := len(enc.Ids); l > maxLen {
			maxLen = l
		}
	}
	batchSize := len(texts)
	inputIDs := make([]int64, batchSize*maxLen)
	attentionMask := make([]int64, batchSize*maxLen)
	for i, enc := range encodings {
		ids := enc.Ids
		offset := i * maxLen
		for j, id := range ids {
			inputIDs[offset+j] = int64(id)
			attentionMask[offset+j] = 1
		}
		// Remaining positions are already zero (padding)
	}
	// Create tensors with shape [batchSize, maxLen]
	inputTensor, _ := onnxruntime_go.NewTensor[int64](
		onnxruntime_go.NewShape(int64(batchSize), int64(maxLen)),
		inputIDs,
	)
	defer func() { _ = inputTensor.Destroy() }()
	maskTensor, _ := onnxruntime_go.NewTensor[int64](
		onnxruntime_go.NewShape(int64(batchSize), int64(maxLen)),
		attentionMask,
	)
	defer func() { _ = maskTensor.Destroy() }()
	outputTensor, _ := onnxruntime_go.NewEmptyTensor[float32](
		onnxruntime_go.NewShape(int64(batchSize), int64(e.dims)),
	)
	defer func() { _ = outputTensor.Destroy() }()
	err := e.session.Run(
		[]onnxruntime_go.Value{inputTensor, maskTensor},
		[]onnxruntime_go.Value{outputTensor},
	)
	if err != nil {
		return nil, err
	}
	// Extract embeddings per batch item
	data := outputTensor.GetData()
	embeddings := make([][]float32, batchSize)
	for i := 0; i < batchSize; i++ {
		start := i * e.dims
		emb := make([]float32, e.dims)
		copy(emb, data[start:start+e.dims])
		embeddings[i] = emb
	}
	return embeddings, nil
}
