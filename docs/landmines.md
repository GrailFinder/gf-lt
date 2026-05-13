# Landmines

This document captures pitfalls and traps encountered during development to help avoid them in the future.

---

## 1. Go Map Random Iteration Order

When using `map[string]interface{}` with `json.Marshal`, Go does not guarantee key ordering. The serialized JSON may have keys in different order on each run.

**Problem:** If code checks for a prefix like `strings.HasPrefix(jsonStr, "{\"type\":\"multimodal_content\"")`, it will fail when `"parts"` happens to serialize before `"type"`.

**Fix:** Use a struct with explicit JSON tags instead of a map:

```go
// Bad - random key order
resp := map[string]interface{}{
    "type": "multimodal_content",
    "parts": []map[string]string{...},
}

// Good - deterministic key order
type multimodalContent struct {
    Type  string              `json:"type"`
    Parts []map[string]string `json:"parts"`
}
resp := multimodalContent{
    Type:  "multimodal_content",
    Parts: []map[string]string{...},
}
```

**Real-world example:** imgen_mcp server was returning `{"parts":[...]}` instead of `{"type":"multimodal_content","parts":[...]}` due to this issue, breaking image handling in gf-lt.

---

## 2. MCP Response Format for Images

MCP tools that return images should return the full `multimodal_content` JSON format, not just a path reference.

**Expected format:**
```json
{
  "type": "multimodal_content",
  "parts": [
    {"type": "text", "text": "Image: /path/to/image.png"},
    {"type": "image_url", "url": "data:image/png;base64,...", "path": "/path/to/image.png"}
  ]
}
```

---

## 3. Two LLM Endpoints, Two Image Handling Paths

gf-lt supports two types of LLM endpoints, each handling images differently:

### `/completion` endpoint (LCPCompletion)
- Uses `FormMsg` at llm.go:172
- Extracts base64 to separate `multimodalData` array
- Adds `<__media__>` marker in prompt text
- Images don't blow up token count in prompt

### `/v1/chat/completions` endpoint (LCPChat)
- Uses `FormMsg` at llm.go:317
- Sends images embedded in `ContentParts` array (OpenAI multimodal format)
- Base64 goes directly in JSON `content` field
- Depends on llama.cpp's chat endpoint to handle multimodal properly

When debugging image-related issues, check which endpoint is being used (see `cfg.CurrentAPI` and `choseChunkParser()` in llm.go).
