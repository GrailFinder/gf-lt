### TODO:
- scrolling chat history; (somewhat works out of box); +
- log errors to file; +
- give serial id to each msg in chat to track it; (use slice index) +
- show msg id next to the msg; +
- regen last message; +
- delete last message; +
- edit message? (including from bot); +
- ability to copy message; +
- menu with old chats (chat files); +
- fullscreen textarea option (for long prompt);
- tab to switch selection between textview and textarea (input and chat); +
- basic tools: memorize and recall;
- stop stream from the bot; +
- sqlitedb instead of chatfiles; +
- sqlite for the bot memory;
- option to switch between predefined sys prompts;

### FIX:
- bot responding (or haninging) blocks everything; +
- programm requires history folder, but it is .gitignore; +
- at first run chat table does not exist; run migrations sql on startup; +
- Tab is needed to copy paste text into textarea box, use shift+tab to switch focus; (changed tp pgup) +
- delete last msg: can have unexpected behavior (deletes what appears to be two messages if last bot msg was not generated (should only delete icon in that case));
- empty input to continue bot msg gens new msg index and bot icon;
