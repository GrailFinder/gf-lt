# Configuration Guide

This document explains how to set up and configure the application using the `config.toml` file. The configuration file controls various aspects of the application including API endpoints, roles, RAG settings, TTS/STT features, and more.

## Getting Started

1. Copy the example configuration file:
   ```bash
   cp config.example.toml config.toml
   ```

2. Edit the `config.toml` file to match your requirements.

## Configuration Options

### API Settings

#### llama.cpp
- **ChatAPI**: The endpoint for chat completions API. This is the primary API used for chat interactions.
- **CompletionAPI**: The endpoint for completion API. Used as an alternative to the chat API.

#### FetchModelNameAPI (`"http://localhost:8080/v1/models"`)
- The endpoint to fetch available models from the API provider.

#### DeepSeek Settings
- **DeepSeekChatAPI**: The endpoint for DeepSeek chat API. Default: `"https://api.deepseek.com/chat/completions"`
- **DeepSeekCompletionAPI**: The endpoint for DeepSeek completion API. Default: `"https://api.deepseek.com/beta/completions"`
- **DeepSeekModel**: The model to use with DeepSeek API. Default: `"deepseek-reasoner"`
- **DeepSeekToken**: Your DeepSeek API token. Uncomment and set this value to enable DeepSeek features.

#### OpenRouter Settings
- **OpenRouterChatAPI**: The endpoint for OpenRouter chat API. Default: `"https://openrouter.ai/api/v1/chat/completions"`
- **OpenRouterCompletionAPI**: The endpoint for OpenRouter completion API. Default: `"https://openrouter.ai/api/v1/completions"`
- **OpenRouterToken**: Your OpenRouter API token. Uncomment and set this value to enable OpenRouter features.

### Role Settings

#### UserRole (`"user"`)
- The role identifier for user messages in the conversation.

#### ToolRole (`"tool"`)
- The role identifier for tool responses in the conversation.

#### AssistantRole (`"assistant"`)
- The role identifier for assistant responses in the conversation.

### Display and Logging Settings

#### ShowSys (`true`)
- Whether to show system and tool messages in the chat interface.

#### LogFile (`"log.txt"`)
- The file path where application logs will be stored.

#### SysDir (`"sysprompts"`)
- Directory containing system prompt templates (character cards).

### Content and Performance Settings

#### ChunkLimit (`100000`)
- Maximum size of text chunks to recieve per request from llm provider. Mainly exists to prevent infinite spam of random or repeated tokens when model starts hallucinating.

#### AutoScrollEnabled (`true`)
- Whether to automatically scroll chat window while llm streams its repsonse.

#### AutoCleanToolCallsFromCtx (`false`)
- Whether to automatically clean tool calls from the conversation context to manage token usage.

### RAG (Retrieval Augmented Generation) Settings

#### EmbedURL (`"http://localhost:8082/v1/embeddings"`)
- The endpoint for embedding API, used for RAG (Retrieval Augmented Generation) functionality.

#### RAGEnabled (`false`)
- Enable or disable RAG functionality for enhanced context retrieval.

#### RAGBatchSize (`1`)
- Number of documents to process in each RAG batch.

#### RAGWordLimit (`80`)
- Maximum number of words in a batch to tokenize and store.

#### RAGWorkers (`2`)
- Number of concurrent workers for RAG processing.

#### RAGDir (`"ragimport"`)
- Directory containing documents for RAG processing.

#### HFToken (`""`)
- Hugging Face token for accessing models and embeddings. In case your embedding model is hosted on hf.


### Text-to-Speech (TTS) Settings

#### TTS_ENABLED (`false`)
- Enable or disable text-to-speech functionality.

#### TTS_URL (`"http://localhost:8880/v1/audio/speech"`)
- The endpoint for TTS API (used with `kokoro` provider).

#### TTS_SPEED (`1.2`)
- Playback speed for speech output (1.0 is normal speed).

#### TTS_PROVIDER (`"kokoro"`)
- TTS provider to use. Options: `"kokoro"` or `"google"`.
  - `"kokoro"`: Uses Kokoro FastAPI TTS server (requires TTS_URL to be set). Provides high-quality voice synthesis but requires a running Kokoro server.
  - `"google"`: Uses Google Translate TTS with gopxl/beep for local playback. Works offline using Google's public TTS API with local audio playback via gopxl/beep. Supports multiple languages via TTS_LANGUAGE setting.

#### TTS_LANGUAGE (`"en"`)
- Language code for TTS (used with `google` provider).
  - Examples: `"en"` (English), `"es"` (Spanish), `"fr"` (French)
  - See Google Translate TTS documentation for supported languages.

### Speech-to-Text (STT) Settings

#### STT_ENABLED (`false`)
- Enable or disable speech-to-text functionality.

#### STT_TYPE (`"WHISPER_SERVER"`)
- Type of STT engine to use. Options are `"WHISPER_SERVER"` or `"WHISPER_BINARY"`. Whisper server is used inside of docker continer, while binary can be local.

#### STT_URL (`"http://localhost:8081/inference"`)
- The endpoint for STT API (used with WHISPER_SERVER).

#### WhisperBinaryPath (`"./batteries/whisper.cpp/build/bin/whisper-cli"`)
- Path to the whisper binary (used with WHISPER_BINARY mode).

#### WhisperModelPath (`"./batteries/whisper.cpp/ggml-large-v3-turbo-q5_0.bin"`)
- Path to the whisper model file (used with WHISPER_BINARY mode).

#### STT_LANG (`"en"`)
- Language for speech recognition (used with WHISPER_BINARY mode).

#### STT_SR (`16000`)
- Sample rate for mic recording.

### Database and File Settings

#### DBPATH (`"gflt.db"`)
- Path to the SQLite database file used for storing conversation history and other data.

#### FilePickerDir (`"."`)
- Directory where the file picker starts and where relative paths in coding assistant file tools (file_read, file_write, etc.) are resolved against. Use absolute paths (starting with `/`) to bypass this.

#### EnableMouse (`false`)
- Enable or disable mouse support in the UI. When set to `true`, allows clicking buttons and interacting with UI elements using the mouse, but prevents the terminal from handling mouse events normally (such as selecting and copying text). When set to `false`, enables default terminal behavior allowing you to select and copy text, but disables mouse interaction with UI elements.

### Character-Specific Context Settings (/completion only)

[character specific context page for more info](./char-specific-context.md)

#### CharSpecificContextEnabled (`true`)
- Enable or disable character-specific context functionality.

#### CharSpecificContextTag (`"@"`)
- The tag prefix used to reference character-specific context in messages.

#### AutoTurn (`true`)
- Enable or disable automatic turn detection/switching.

### Additional Features

Those could be switched in program, but also bould be setup in config.

#### ToolUse
- Enable or disable explanation of tools to llm, so it could use them.

### StripThinkingFromAPI (`true`)
- Strip thinking blocks from messages before sending to LLM. Keeps them in chat history for local viewing but reduces token usage in API calls.

#### ReasoningEffort (`"medium"`)
- OpenRouter reasoning configuration (only applies to OpenRouter chat API). Valid values: `xhigh`, `high`, `medium`, `low`, `minimal`, `none`. Empty or `none` disables reasoning.

## Environment Variables

The application supports using environment variables for API keys as fallbacks:

- `OPENROUTER_API_KEY`: Used if `OpenRouterToken` is not set in the config
- `DEEPSEEK_API_KEY`: Used if `DeepSeekToken` is not set in the config
