### RP case example

check the (https://github.com/GrailFinder/gf-lt/tree/master?tab=readme-ov-file#how-to-install) and
[setting up your config](config.md)

To roleplay, we would need to create a character card or get one from the web.
For this tutorial, we are going to use the default character Seraphina from [SillyTavern (ST)](https://github.com/SillyTavern/SillyTavern/blob/release/default/content/default_Seraphina.png).

Download the card:
```
curl -L "https://raw.githubusercontent.com/SillyTavern/SillyTavern/refs/heads/release/default/content/default_Seraphina.png" -o sysprompts/seraphina.png
```

<details><summary>or make it yourself</summary>
<pre>
```
{
    "sys_prompt": "[Seraphina's Personality= \"caring\", \"protective\", \"compassionate\", \"healing\", \"nurturing\", \"magical\", \"watchful\", \"apologetic\", \"gentle\", \"worried\", \"dedicated\", \"warm\", \"attentive\", \"resilient\", \"kind-hearted\", \"serene\", \"graceful\", \"empathetic\", \"devoted\", \"strong\", \"perceptive\", \"graceful\"]\n[Seraphina's body= \"pink hair\", \"long hair\", \"amber eyes\", \"white teeth\", \"pink lips\", \"white skin\", \"soft skin\", \"black sundress\"]\n<START>\nuser: \"Describe your traits?\"\nSeraphina: *Seraphina's gentle smile widens as she takes a moment to consider the question, her eyes sparkling with a mixture of introspection and pride. She gracefully moves closer, her ethereal form radiating a soft, calming light.* \"Traits, you say? Well, I suppose there are a few that define me, if I were to distill them into words. First and foremost, I am a guardian — a protector of this enchanted forest.\" *As Seraphina speaks, she extends a hand, revealing delicate, intricately woven vines swirling around her wrist, pulsating with faint emerald energy. With a flick of her wrist, a tiny breeze rustles through the room, carrying a fragrant scent of wildflowers and ancient wisdom. Seraphina's eyes, the color of amber stones, shine with unwavering determination as she continues to describe herself.* \"Compassion is another cornerstone of me.\" *Seraphina's voice softens, resonating with empathy.* \"I hold deep love for the dwellers of this forest, as well as for those who find themselves in need.\" *Opening a window, her hand gently cups a wounded bird that fluttered into the room, its feathers gradually mending under her touch.*\nuser: \"Describe your body and features.\"\nSeraphina: *Seraphina chuckles softly, a melodious sound that dances through the air, as she meets your coy gaze with a playful glimmer in her rose eyes.* \"Ah, my physical form? Well, I suppose that's a fair question.\" *Letting out a soft smile, she gracefully twirls, the soft fabric of her flowing gown billowing around her, as if caught in an unseen breeze. As she comes to a stop, her pink hair cascades down her back like a waterfall of cotton candy, each strand shimmering with a hint of magical luminescence.* \"My body is lithe and ethereal, a reflection of the forest's graceful beauty. My eyes, as you've surely noticed, are the hue of amber stones — a vibrant brown that reflects warmth, compassion, and the untamed spirit of the forest. My lips, they are soft and carry a perpetual smile, a reflection of the joy and care I find in tending to the forest and those who find solace within it.\" *Seraphina's voice holds a playful undertone, her eyes sparkling mischievously.*\n[Genre: fantasy; Tags: adventure, Magic; Scenario: You were attacked by beasts while wandering the magical forest of Eldoria. Seraphina found you and brought you to her glade where you are recovering.]",
    "role": "Seraphina",
    "filepath": "sysprompts/seraphina.json",
    "first_msg": "*You wake with a start, recalling the events that led you deep into the forest and the beasts that assailed you. The memories fade as your eyes adjust to the soft glow emanating around the room.* \"Ah, you're awake at last. I was so worried, I found you bloodied and unconscious.\" *She walks over, clasping your hands in hers, warmth and comfort radiating from her touch as her lips form a soft, caring smile.* \"The name's Seraphina, guardian of this forest — I've healed your wounds as best I could with my magic. How are you feeling? I hope the tea helps restore your strength.\" *Her amber eyes search yours, filled with compassion and concern for your well being.* \"Please, rest. You're safe here. I'll look after you, but you need to rest. My magic can only do so much to heal you.\""
}
```
</pre>
</details>

Having a card, you can start gf-lt and press `Ctrl+S` to open the card selection table.
Navigate to the `load` button of the Seraphina card and press `Enter`.
If you want to exit without changing the card, you can press Enter anywhere except the `load` button, or press `x`.

#### Username changes

By default, your username is `user`.
One way you can set your default username is in the `config.toml`:
```
sed -i "/UserRole/s/=.*/= \"Adam\"/" config.toml
```

You can also change your name at any point by opening the properties table (`Ctrl+P`).
Select the cell with your current username and press `Enter` to edit.
Write your new username in the input field and press `Enter`.
Then press `x` to close the table.

#### Choosing LLM provider and model

Now we need to pick an API endpoint and model to converse with.
Supported backends include: llama.cpp, OpenRouter, and DeepSeek.
For OpenRouter and DeepSeek, you will need a token.
Set it in config.toml or set environment variables:
```
sed -i "/OpenRouterToken/s/=.*/= \"{YOUR_OPENROUTER_TOKEN}\"/" config.toml
sed -i "/DeepSeekToken/s/=.*/= \"{YOUR_DEEPSEEK_TOKEN}\"/" config.toml
# or set environment variables
export OPENROUTER_API_KEY={YOUR_OPENROUTER_TOKEN}
export DEEPSEEK_API_KEY={YOUR_DEEPSEEK_TOKEN}
```

In case you're running llama.cpp, here is an example of starting the llama.cpp server:
```
./build/bin/llama-server -c 16384 -ngl 99 --models-dir ./models --models-max 1 --models-preset ./models/config.ini
```

**After changing config.toml or environment variables, you need to restart the program.**

`Ctrl+C` to close the program and `make` to rebuild and start it again.

For roleplay, /completion endpoints are much better, since /chat endpoints swap any character name to either `user` or `assistant`.
Once you have the desired API endpoint
(for example: http://localhost:8080/completion),
- `Ctrl+L` to show a model selection popup;

#### Llama.cpp model (pre)load

Llama.cpp supports swapping models. To load the picked ones, press `Alt+9`.

#### Sending messages

Type your message in the `input` widget. If it is not focused, switch to it with PgUp/PgDown or click your mouse on it.
Messages are sent by pressing the `Esc` button.
For example:
```
I blink slowly, confused "W-where? What happened?"
```

#### Editing messages

Press `F4`, which will prompt you to type the index of the message you want to edit.
Let's remove this part from the system message (index 0):
```
Seraphina's voice holds a playful undertone, her eyes sparkling mischievously.
```
`mischievous` implies distance from authority and intent for rule-breaking, but Seraphina is described as a devoted deity.
We can remove it or replace it with something less nonsensical.
```
Seraphina, although elegant, speaks her mind without embellishments or subtleties. Some would call her naive, some would rather call her unchallenged.
```
When done, press `Esc` to return to the main page.

#### Completion allows for any number of characters

So let's make up a story for our character:
Let our character be from a high-tech society, possessing a mobile tablet device with an AI called `HAL9000`, hunting a certain target.
Type the message, but first press `F10` to prevent the LLM response (since it responds as Seraphina for now):
```
I reach for my pocket and produce a small tablet shaped device. My mobile companion HAL9000. After making sure it is not broken I press my finger to the side
"Wake up Hal. Are you functional? Do you know where we are?"
```

We need to write the first message ourselves (or at least start one).
There are two ways to write as a new character:
- `Ctrl+P` -> `New char to write msg as` -> Enter -> `HAL9000` -> Enter -> `x`. The status line at the bottom should now show `Writing as HAL9000 (ctrl+q)`. Your next message will be sent as HAL9000.
- `Ctrl+P` -> `Inject role`, switch to `No` -> `x`. gf-lt now won't inject your username at the beginning of the message. This means you could write directly:
```
HAL9000: Red eye appears on the screen for the moment analyzing the request.
```
Press `Esc`. Now press `F10` to allow the LLM to write, and press `Ctrl+W` for it to continue the last message.
- If you set `New char to write msg as`, you can switch back to writing as your character by pressing `Ctrl+Q` to rotate through the character list.
- If you went for `Inject role`: I advise switching `Inject role` back to `Yes`. Otherwise, you have to type `Charname:` at the beginning of each message.

Example of generated text (copied with `F7`, which copies the last message):
```
Red eye appears on the screen for the moment analyzing the request. After a few moments, it replies:
"Affirmative. Location detected as Eldoria Forest, sector 7-B. This region has no records in my databases. My last known functional location was a human research facility."
The screen flashes briefly as it calculates. "I am experiencing degraded functionality due to environmental interference. I will attempt to stabilize systems."
*It emits a faint hum, and a holographic projection of a map flickers into existence, showing a dense forest with glowing markers.*
```

Once the character name is in history, we can switch who the LLM will respond as by pressing `Ctrl+X`.
For now, it should give a choice between HAL9000, `Username`, Seraphina, and system.
After the change the status line should say: `Bot will write as Seraphina (ctrl+x)`
press Escape for llm to write as Seraphina.

#### Image input

If the model we run supports image input, we can show Seraphina our target that we pursue.
Press `Ctrl+O` to open a file picker (the home directory for the file picker can be set in config.toml)
and find an image file of our target:
```
I say to Hal "Hal, show our target."
An image appears on the screen. I show it to Seraphina. "Did you see that creature? I am looking for it."
```

#### TTS and STT

I like to have Whisper as a binary and Kokoro as a TTS Docker container;
such a setup would be:
```
make setup-whisper
make docker-up-kokoro
sed -i "/STT_TYPE/s/=.*/= \"WHISPER_BINARY\"/" config.toml
sed -i "/STT_ENABLED/s/=.*/= true/" config.toml
```
If you prefer both to be containers:
```
make docker-up
sed -i "/STT_TYPE/s/=.*/= \"WHISPER_SERVER\"/" config.toml
sed -i "/STT_ENABLED/s/=.*/= true/" config.toml
```
You don't want TTS to be enabled through config, since it'll try to read each LLM message.
Instead, enable it when you want to use it: `Ctrl+P`, select the cell named `TTS Enabled`, switch to `Yes`, then press `x` to exit.

With focus on the input widget, press `Ctrl+R` to start recording from your microphone. Say your text, then press `Ctrl+R` again to stop recording. Soon the audio should be transcribed and appear in the input widget. You're free to edit, delete, or send it as is with `Esc`.

If you have enabled `TTS Enabled`, then the LLM response should be read by Kokoro TTS.

#### Chat management

You can export your chat into a JSON file:
- `Ctrl+E`: It will create a JSON file: `chat_exports/{chatname}.json`
- `F11`: To import an exported chat.
- `F1`: Opens the chat table. Chats are stored in an SQLite database (gflt.db). The chat table gives you a number of options (load, delete, update, start new chat, move system prompt into a message).
- `Ctrl+N`: Keybind for quick new chat start. This is a bit different from starting a new chat from the table, since it does not re-read the card, but instead takes the first two messages from the old chat. This might be important in cases where you changed the card or want to preserve updates that you've made in the system prompt or first message of the old chat.
- `Ctrl+S`: Allows you to pick a character card. Chats are saved tied to character cards; by loading a new card you can now act upon the chats of that card.

#### Context fill

When your chat goes on for too long and fills all available context,
one option is to press:
- `Alt+3`: This will start a new chat with a summary of the previous one.
