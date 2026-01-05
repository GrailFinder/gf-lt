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

#### FetchModelNameAPI
- **Type**: String
- **Default**: `"http://localhost:8080/v1/models"`
- **Description**: The endpoint to fetch available models from the API provider.

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

#### UserRole
- **Type**: String
- **Default**: `"user"`
- **Description**: The role identifier for user messages in the conversation.

#### ToolRole
- **Type**: String
- **Default**: `"tool"`
- **Description**: The role identifier for tool responses in the conversation.

#### AssistantRole
- **Type**: String
- **Default**: `"assistant"`
- **Description**: The role identifier for assistant responses in the conversation.

### Display and Logging Settings

#### ShowSys
- **Type**: Boolean
- **Default**: `true`
- **Description**: Whether to show system and tool messages in the chat interface.

#### LogFile
- **Type**: String
- **Default**: `"log.txt"`
- **Description**: The file path where application logs will be stored.

#### SysDir
- **Type**: String
- **Default**: `"sysprompts"`
- **Description**: Directory containing system prompt templates (character cards).

### Content and Performance Settings

#### ChunkLimit
- **Type**: Integer
- **Default**: `100000`
- **Description**: Maximum size of text chunks to recieve per request from llm provider. Mainly exists to prevent infinite spam of random or repeated tokens when model starts hallucinating.

#### AutoScrollEnabled
- **Type**: Boolean
- **Default**: `true`
- **Description**: Whether to automatically scroll chat window while llm streams its repsonse.

#### AutoCleanToolCallsFromCtx
- **Type**: Boolean
- **Default**: `false` (commented out)
- **Description**: Whether to automatically clean tool calls from the conversation context to manage token usage.

### RAG (Retrieval Augmented Generation) Settings

#### EmbedURL
- **Type**: String
- **Default**: `"http://localhost:8082/v1/embeddings"`
- **Description**: The endpoint for embedding API, used for RAG (Retrieval Augmented Generation) functionality.

#### RAGEnabled
- **Type**: Boolean
- **Default**: Not set in example (false by default)
- **Description**: Enable or disable RAG functionality for enhanced context retrieval.

#### RAGBatchSize
- **Type**: Integer
- **Default**: `1`
- **Description**: Number of documents to process in each RAG batch.

#### RAGWordLimit
- **Type**: Integer
- **Default**: `80`
- **Description**: Maximum number of words to include in RAG context.

#### RAGWorkers
- **Type**: Integer
- **Default**: `2`
- **Description**: Number of concurrent workers for RAG processing.

#### RAGDir
- **Type**: String
- **Default**: `"ragimport"`
- **Description**: Directory containing documents for RAG processing.

#### HFToken
- **Type**: String
- **Default**: Not set in example
- **Description**: Hugging Face token for accessing models and embeddings. In case your embedding model is hosted on hf.


### Text-to-Speech (TTS) Settings

#### TTS_ENABLED
- **Type**: Boolean
- **Default**: `false`
- **Description**: Enable or disable text-to-speech functionality.

#### TTS_URL
- **Type**: String
- **Default**: `"http://localhost:8880/v1/audio/speech"`
- **Description**: The endpoint for TTS API.

#### TTS_SPEED
- **Type**: Float
- **Default**: `1.2`
- **Description**: Playback speed for speech output (1.0 is normal speed).

### Speech-to-Text (STT) Settings

#### STT_ENABLED
- **Type**: Boolean
- **Default**: `false`
- **Description**: Enable or disable speech-to-text functionality.

#### STT_TYPE
- **Type**: String
- **Default**: `"WHISPER_SERVER"`
- **Description**: Type of STT engine to use. Options are `"WHISPER_SERVER"` or `"WHISPER_BINARY"`. Whisper server is used inside of docker continer, while binary can be local.

#### STT_URL
- **Type**: String
- **Default**: `"http://localhost:8081/inference"`
- **Description**: The endpoint for STT API (used with WHISPER_SERVER).

#### WhisperBinaryPath
- **Type**: String
- **Default**: `"./batteries/whisper.cpp/build/bin/whisper-cli"`
- **Description**: Path to the whisper binary (used with WHISPER_BINARY mode).

#### WhisperModelPath
- **Type**: String
- **Default**: `"./batteries/whisper.cpp/ggml-large-v3-turbo-q5_0.bin"`
- **Description**: Path to the whisper model file (used with WHISPER_BINARY mode).

#### STT_LANG
- **Type**: String
- **Default**: `"en"`
- **Description**: Language for speech recognition (used with WHISPER_BINARY mode).

#### STT_SR
- **Type**: Integer
- **Default**: `16000`
- **Description**: Sample rate for mic recording.

### Database and File Settings

#### DBPATH
- **Type**: String
- **Default**: `"gflt.db"`
- **Description**: Path to the SQLite database file used for storing conversation history and other data.

#### FilePickerDir
- **Type**: String
- **Default**: `"."`
- **Description**: Directory where the file (image) picker should start when selecting files.

#### FilePickerExts
- **Type**: String
- **Default**: `"png,jpg,jpeg,gif,webp"`
- **Description**: Comma-separated list of allowed file extensions for the file picker.

### Additional Features

Those could be switched in program, but also bould be setup in config.

#### ToolUse
- **Type**: Boolean
- **Default**: Not set in example (false by default)
- **Description**: Enable or disable explanation of tools to llm, so it could use them.

#### ThinkUse
- **Type**: Boolean
- **Default**: Not set in example (false by default)
- **Description**: Enable or disable insertion of <think> token at the beggining of llm resp.

## Environment Variables

The application supports using environment variables for API keys as fallbacks:

- `OPENROUTER_API_KEY`: Used if `OpenRouterToken` is not set in the config
- `DEEPSEEK_API_KEY`: Used if `DeepSeekToken` is not set in the config
