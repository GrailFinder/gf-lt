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

### RAG (Retrieval Augmented Generation) Settings

#### EmbedURL (`"http://localhost:8082/v1/embeddings"`)
- The endpoint for embedding API, used for RAG (Retrieval Augmented Generation) functionality.

#### RAGBatchSize (`1`)
- Number of documents to process in each RAG batch.

#### RAGWordLimit (`80`)
- Maximum number of words in a batch to tokenize and store.

#### RAGDir (`"ragimport"`)
- Directory containing documents for RAG processing.

#### HFToken (`""`)
- Hugging Face token for accessing models and embeddings. In case your embedding model is hosted on hf.


### Text-to-Speech (TTS) Settings

#### TTS_ENABLED (`false`)
- Enable or disable text-to-speech functionality.

#### TTS_URL (`"http://localhost:8880/v1/audio/speech"`)
- The endpoint for the TTS API (OpenAI `/v1/audio/speech` format).

#### TTS_SPEED (`1.2`)
- Playback speed for speech output (1.0 is normal speed).

#### TTS_PROVIDER (`"openai"`)
- TTS provider to use. Options: `"openai"`, `"kokoro"` (alias for `"openai"`), or `"google"`.
  - `"openai"` / `"kokoro"`: Uses any OpenAI-compatible TTS API (requires TTS_URL to be set). Provides high-quality voice synthesis.
  - `"google"`: Uses Google Translate TTS for local playback. Works offline using Google's public TTS API. Supports multiple languages via TTS_LANGUAGE setting.

#### TTS_VOICE (`""`)
- Voice name passed to the OpenAI-compatible TTS API. Examples: `"af_bella"`, `"alloy"`, `"echo"`, `"fable"`, `"onyx"`, `"nova"`, `"shimmer"`. If empty, the server default is used.

#### TTS_MODEL (`""`)
- Model name passed to the OpenAI-compatible TTS API. Defaults to `"tts-1"` if empty.

#### TTS_LANGUAGE (`"en"`)
- Language code for TTS (used with `google` provider).
  - Examples: `"en"` (English), `"es"` (Spanish), `"fr"` (French)
  - See Google Translate TTS documentation for supported languages.

### Speech-to-Text (STT) Settings

#### STT_ENABLED (`false`)
- Enable or disable speech-to-text functionality.

#### STT_TYPE (`"OPENAI_COMPAT"`)
- Type of STT engine. `"OPENAI_COMPAT"` is the only backend; it speaks the OpenAI
  `POST /v1/audio/transcriptions` API, so any compatible server works (CrispASR in
  `--server` mode, whisper.cpp's `whisper-server`, etc.). Aliases: `openai_compat`, `crispasr`.

#### STT_URL (`"http://localhost:8085"`)
- Base URL of the STT server. `/v1/audio/transcriptions` is appended automatically, so give
  the bare host:port, not the full path.

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

#### Playwright Browser Automation
These settings enable browser automation tools available to the LLM.

- **PlaywrightEnabled** (`false`)
  - Enable or disable Playwright browser automation tools for the LLM. When enabled, the LLM can use tools like `pw_browser`, `pw_close`, and `pw_status` to automate browser interactions.

- **PlaywrightDebug** (`false`)
  - Enable debug mode for Playwright browser. When set to `true`, the browser runs in visible (non-headless) mode, displaying the GUI for debugging purposes. When `false`, the browser runs in headless mode by default.

### StripThinkingFromAPI (`true`)
- Controls only what is *sent to the LLM*. When `true`, `<think>` blocks are removed from
  assistant messages in outgoing requests (saves tokens; the blocks remain in local chat
  history and display). Set to `false` to keep thinking in the context sent to the model —
  some reasoning models follow instructions better when their previous thinking is preserved.

### StoreThinking (`true`)
- Controls only what is *kept locally*. When `true` (default), `<think>` blocks are stored in
  `chatBody`, persisted chats, exports, and shown in the UI. When `false`, thinking blocks are
  discarded as soon as the response is complete, keeping history compact.

The two options are independent: all four combinations are valid (e.g. `StoreThinking = false`
with `StripThinkingFromAPI = false` means thinking is shown live but kept nowhere).

#### ReasoningEffort (`"medium"`)
- OpenRouter reasoning configuration (only applies to OpenRouter chat API). Valid values: `xhigh`, `high`, `medium`, `low`, `minimal`, `none`. Empty or `none` disables reasoning.

## Environment Variables

The application supports using environment variables for API keys as fallbacks:

- `OPENROUTER_API_KEY`: Used if `OpenRouterToken` is not set in the config
- `DEEPSEEK_API_KEY`: Used if `DeepSeekToken` is not set in the config
