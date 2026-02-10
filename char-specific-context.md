# Character-Specific Context

**/completion only feature; won't work with /v1/chat**

## Overview

Character-Specific Context is a feature that enables private communication between characters in a multi-character chat. When enabled, messages can be tagged with a special marker indicating which characters should "know" about (see) that message. This allows for secret conversations, private information sharing, and roleplaying scenarios where certain characters are not privy to all communications.

(This feature works by filtering the chat history for each character based on the `KnownTo` field associated with each message. Only messages that are intended for a particular character (or are public) are included in that character's view of the conversation.)

## How It Works

### Tagging Messages

Messages can be tagged with a special string (by default `@`) followed by a comma-separated list of character names. The tag can appear anywhere in the message content. **After csv of characters tag should be closed with `@` (for regexp to know where it ends).**

**Example:**
```
Alice: @Bob@ Can you keep a secret?
```

**To avoid breaking immersion, it is better to place the tag in (ooc:)**
```
Alice: (ooc: @Bob@) Can you keep a secret?
```

This message will be visible only to Alice (the sender) and Bob. The tag is parsed by `parseKnownToTag` and the resulting list of character names is stored in the `KnownTo` field of the message (`RoleMsg`). The sender is automatically added to the `KnownTo` list (if not already present) by `processMessageTag`.

Multiple tags can be used in a single message; all mentioned characters are combined into the `KnownTo` list.

### Filtering Chat History

When it's a character's turn to respond, the function `filterMessagesForCharacter` filters the full message list, returning only those messages where:

- `KnownTo` is empty (message is public), OR
- `KnownTo` contains the character's name.

System messages (`role == "system"`) are always visible to all characters.

The filtered history is then used to construct the prompt sent to the LLM. This ensures each character only sees messages they are supposed to know about.

### Configuration

Two configuration settings control this feature:

- `CharSpecificContextEnabled` – boolean; enables or disables the feature globally.
- `CharSpecificContextTag` – string; the tag used to mark private messages. Default is `@`.

These are set in `config.toml` (see `config.example.toml` for the default values).

### Processing Pipeline

1. **Message Creation** – When a message is added to the chat (by a user or LLM), `processMessageTag` scans its content for the known‑to tag.
2. **Storage** – The parsed `KnownTo` list is stored with the message in the database.
3. **Filtering** – Whenever the chat history is needed (e.g., for an LLM request), `filterMessagesForCharacter` is called with the target character (the one whose turn it is). The filtered list is used for the prompt.
4. **Display** – The TUI also uses the same filtering when showing the conversation for a selected character (see “Writing as…”).

## Usage Examples

### Basic Private Message

Alice wants to tell Bob something without Carl knowing:

```
Alice: @Bob@ Meet me at the library tonight.
```

Result:
- Alice (sender) sees the message.
- Bob sees the message.
- Carl does **not** see the message in his chat history.

### Multi‑recipient Secret

Alice shares a secret with Bob and Carl, but not David:

```
Alice: (ooc: @Bob,Carl@) The treasure is hidden under the old oak.
```

### Public Message

A message without any tag (or with an empty `KnownTo`) is visible to all characters.

```
Alice: Hello everyone!
```

### User‑Role Considerations

The human user can assume any character’s identity via the “Writing as…” feature (`cfg.UserRole` and `cfg.WriteNextMsgAs`). When the user writes as a character, the same filtering rules apply: the user will see only the messages that character would see.

## Interaction with AutoTurn and WriteNextMsgAsCompletionAgent

### WriteNextMsgAsCompletionAgent

This configuration variable determines which character the LLM should respond as. It is used by `filterMessagesForCurrentCharacter` to select the target character for filtering. If `WriteNextMsgAsCompletionAgent` is set, the LLM will reply in the voice of that character, and only messages visible to that character will be included in the prompt.

### AutoTurn

Normally llm and user (human) take turns writting messages. With private messages there is an issue, where llm can write a private message that will not be visible for character who user controls, so for a human it would appear that llm did not respond. It is desirable in this case, for llm to answer to itself, larping as target character for that private message.

When `AutoTurn` is enabled, the system can automatically trigger responses from llm as characters who have received a private message. The logic in `triggerPrivateMessageResponses` checks the `KnownTo` list of the last message and, for each recipient that is not the user (or the sender), queues a chat round for that character. This creates a chain of private replies without user intervention.

**Example flow:**
1. Alice (llm) sends a private message to Bob (llm) (`KnownTo = ["Alice","Bob"]`).
2. Carl (user) sees nothing.
3. `AutoTurn` detects this and queues a response from Bob.
4. Bob replies (potentially also privately).
5. The conversation continues automatically until public message is made, or Carl (user) was included in `KnownTo`.


## Cardmaking with multiple characters

So far only json format supports multiple characters.
Card example:
```
{
  "sys_prompt": "This is a chat between Alice, Bob and Carl. Normally what is said by any character is seen by all others. But characters also might write messages intended to specific targets if their message contain string tag '@{CharName1,CharName2,CharName3}@'.\nFor example:\nAlice:\n\"Hey, Bob. I have a secret for you... (ooc: @Bob@)\"\nThis message would be seen only by Bob and Alice (sender always sees their own message).",
  "role": "Alice",
  "filepath": "sysprompts/alice_bob_carl.json",
  "chars": ["Alice", "Bob", "Carl"],
  "first_msg": "Hey guys! Want to play Alias like game? I'll tell Bob a word and he needs to describe that word so Carl can guess what it was?"
}
```

## Limitations & Caveats

### Endpoint Compatibility

Character‑specific context relies on the `/completion` endpoint (or other completion‑style endpoints) where the LLM is presented with a raw text prompt containing the entire filtered history. It does **not** work with OpenAI‑style `/v1/chat/completions` endpoints, because those endpoints enforce a fixed role set (`user`/`assistant`/`system`) and strip custom role names and metadata.

### TTS
Although text message might be hidden from user character. If TTS is enabled it will be read.

### Tag Parsing

- The tag is case‑sensitive.
- Whitespace around character names is trimmed.
- If the tag appears multiple times, all mentioned characters are combined.

### Database Storage

The `KnownTo` field is stored as a JSON array in the database. Existing messages that were created before enabling the feature will have an empty `KnownTo` and thus be visible to all characters.

## Relevant Configuration

```toml
CharSpecificContextEnabled = true
CharSpecificContextTag = "@"
AutoTurn = false
```
