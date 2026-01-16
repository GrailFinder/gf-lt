say we have a chat (system card) with three or more characters:
Alice, Bob and Carl.
the chat uses /completion endpoint (as oposed to /v1/chat/completion of openai) to the same llm on all chars.
Alice needs to pass info to Bob without Carl knowing the content (or perhaps even that communication occured at all).
Issue is that being in the same chat history (chatBody), llm shares context for each char.
Even if message passed through the tool calls, Carl can see a tool call with the arguments.
If we delete tool calls and their responses, then both Bob and Alice would have to re-request that secret info each time it is their turn, which is absurd.

concept of char specific context:
let every message to have a `KnownTo` field (type []string);
which could be empty (to everyone) or have speicifc names ([]string{"Alice", "Bob"})
so when that's character turn (which we track in `WriteNextMsgAsCompletionAgent`, then that message is injected at its proper index position (means every message should know it's index?) into chatBody (chat history).

indexes are tricky.
what happens if msg is deleted? will every following message decrement their index? so far edit/copy functionality take in consideration position of existing messages in order.
how to avoid two messages with the same index? if Alices letter is send as secret and assigned index: 5. Then Carl's turn we have that secret message excluded, so his action would get also index 5.
Perhaps instead of indexes we should only keep message order by timestamps (time.Time)?

so we need to think of some sort of tag that llm could add into the message, to make sure it is to be known by that specific target char, some weird string that would not occur naturally, that we could parse:
__known_to_chars__Alice,Bob__


for ex.
Alice: __known_to_chars__Bob__ Can you keep a secret?
Bob: I also have a secret for you Alice __known_to_chars__Alice__

tag can be anywhere in the message. Sender should be also included in KnownTo, so we should parse sender name and add them to KnownTo.

also need to consider user case (as in human chatting with llm). User also can assume any char identity to write the message and ideally the same rules should affect user's chars.

Again, this is not going to work with openais /v1/chat endpoint since it converts all characters to user/assistant; so it is completion only feature. It also might cause unwanted effects, so we better have an option in config to switch this context editing on/off.
