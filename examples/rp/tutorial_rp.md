after ![installing](linktoinstruciton) 
![setup your config](link)

to rp we would need to make a card or to get it from the web.
for this tutorial we are going to use default character from [ST](https://github.com/SillyTavern/SillyTavern/blob/release/default/content/default_Seraphina.png) Serhaphina.

download the card
```
curl -L "https://raw.githubusercontent.com/SillyTavern/SillyTavern/refs/heads/release/default/content/default_Seraphina.png" -o sysprompts/seraphina.png
```

<details><summary>or make it yourself</sumary>
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

having a card, you can start gf-lt and pres `ctrl+s` to open card selection table
move against `load` button of seraphina card and press `Enter` again.
if you want to exit without changing card, you can press enter anywhere but `load` button, or press `x`

#### username changes

by default your username is `user`
one way you can set your default username in the `config.toml`
```
sed -i "/UserRole/s/=.*/= \"Adam\"/" config.toml
```

you also can change your name at any point by opening props table (`ctrl+p`)
select cell with your current Username and press `Enter` to edit
write your new username in input field and press `Enter`
then press `x` to close the table.


#### choosing LLM provider and model
now we need to pick API endpoint and model to converse with.
supports backends: llama.cpp, openrouter and deepseek.
for openrouter and deepseek you will need a token.
set it in config.toml or set envvar
```
sed -i "/OpenRouterToken/s/=.*/= \"{YOUR_OPENROUTER_TOKEN}\"/" config.toml
sed -i "/DeepSeekToken/s/=.*/= \"{YOUR_DEEPSEEK_TOKEN}\"/" config.toml
# or set envvar
export OPENROUTER_API_KEY={YOUR_OPENROUTER_TOKEN}
export DEEPSEEK_API_KEY={YOUR_DEEPSEEK_TOKEN}
```

in case you're running llama.cpp here is an example of starting llama.cpp
```
./build/bin/llama-server -c 16384 -ngl 99 --models-dir ./models --models-max 1 --models-preset ./models/config.ini
```

<b>after changing config.toml or envvar you need to restart the program.</b>

for RP /completion endpoints are much better, since /chat endpoints swap any character name to either `user` or `assistant`;
once you have desired API endpoint  
(for example: http://localhost:8080/completion)  
there are two ways to pick a model: 
- `ctrl+l` allows you to iterate through model list while in main window.
- `ctrl+p` (opens props table) go to the `Select a model` row and press enter, list of available models would appear, pick any that you want, press `x` to exit the props table.

#### llama.cpp model preload
llama.cpp supports swapping models, to load picked ones press `alt+9`

#### sending messages
type your message in the `input` widget; if it is not focused switch to it with pgup/pgdown or click your mouse on it.
messages are send by pressing `Esc` button
for ex.
```
I blink slowly, confused "W-where? What happened?"
```

#### editing messages
press `f4` which'll prompt you to type index of the message you want to edit;
let's remove this part from sysmsg (0)
```
Seraphina's voice holds a playful undertone, her eyes sparkling mischievously.
```
`mischievous` imply distance from authority and intent for the rule breaking, but Seraphina described as devoted deity.
we can remove it or replace it with something less nonsensical.
```
Seraphina, altough elegant, speaks her mind without embellishments or subtleties. Some would call her naive, some would rather call her unchallenged.
```
when done, press `Esc` to return to main page.

#### completion allowes for any number of chars
so let us make up story of our character;
let our character to be from high tech society who has mobile tablet device with AI called `HALL9000`, hunting certain target.
type the message, but first press f10 to prevent llm response (since it responds as Seraphina for now)
```
I reach for my pocket and produce a small tablet shaped device. My mobile companion HALL9000. After making sure it is not broken I press my finger to the side
"Wake up Hal. Are you functional? Do you know where we are?"
```

we need to write first message ourselves (or at least start one)
there are two ways to write as a new char:
- ctrl+p -> `New char to write msg as` -> Enter -> `HAL9000` -> `Enter` -> `x` ; status line at the bottom should now have `Writing as HALL9000 (ctrl+q)` -> your next message would be send as HALL9000.
- ctrl+p -> `Inject role` switch to `No` -> `x`. gf-lt now won't inject your username in beginning of the message. It means you could write directly
```
HAL9000: Red eye appears on the screen for the moment analyzing the request.
```
`Esc`; now press `f10` to allow llm to write and press `ctrl+w` for it to continue the last message.
- if you set `New chat to write msg as`; you can switch back to writing as your char by pressing ctrl+q to rotate through the character list.
- if you went for `Inject role`: I advice to switch `Inject role` back to `Yes`. Otherwise you have to type `Charname:` in the beginning of each message.
example of gen (copied with `f7` (copies last msg))
```
Red eye appears on the screen for the moment analyzing the request. After a few moments, it replies:
"Affirmative. Location detected as Eldoria Forest, sector 7-B. This region has no records in my databases. My last known functional location was a human research facility."
The screen flashes briefly as it calculates. "I am experiencing degraded functionality due to environmental interference. I will attempt to stabilize systems."
*It emits a faint hum, and a holographic projection of a map flickers into existence, showing a dense forest with glowing markers.*
```

Once character name is in history we can switch who llm will respond as pressing `ctrl+x`.
For now it should be rotating between HALL9000, `Username`, Seraphina, system.
make status line to mention: `Bot will write as Seraphina (ctrl+x)`
and press escape to see her reaction.

#### image input
if the model we run support image input we can Seraphina our target that we pursue
press `ctrl+o` to open a filepicker (home directory for filepicker could be set in config.toml)
and find an image file of our target
```
I say to Hal "Hal, show our target."
An image appears on the screen. I show it to Seraphina. "Did you see that creature? I am looking for it."
```

#### tts and stt
I like to have whisper as a binary and kokoro as tts docker container;
such setup would be
```
make setup-whisper
make docker-up-kokoro
sed -i "/STT_TYPE/s/=.*/= \"WHISPER_BINARY\"/" config.toml
sed -i "/STT_ENABLED/s/=.*/= true/" config.toml
```
if you prefer both to be containers
```
make docker-up
sed -i "/STT_TYPE/s/=.*/= \"WHISPER_SERVER\"/" config.toml
sed -i "/STT_ENABLED/s/=.*/= true/" config.toml
```
you don't want TTS be enabled through config, since it'll try to read each llm message.
instead, enable it when you want to use it `ctrl+p` cell named `TTS Enabled` switch to `Yes` -> `x` to exit.

with focus on the input widget press `ctrl+r` which will start recording from your mic. Say your text and press `ctrl+r` again to stop recording. Soon the audio should be transcribe and appear in the input widget. You're free to edit, delete or send it as is with `Esc`.

if you have enabled `TTS Enabled` then llm response should be read by kokoro tts.

#### chat management
you can export your chat into a json file:  
- `ctrl+e`
it will create a json file: `chat_exports/{chatname}.json`
- `f11`
to import exported chat;
- `f1`
opens the chat table, chats are stored in sqlite database (gflt.db);
chat table gives you number of options (load, delete, update, start new chat, move sys prompt into msg);
- `ctrl+n`
keybind for quick new chat start. It is a bit different from new chat from table, since it does not re-read the card, but instead takes first two messages from old chat. It might be important in cases where you changed the card or want to preserve updates that you've made in sysprompt or first message of old chat.
- `ctrl+s`
allowes you to pick a character card. chats are saved tied to character cards, by loading new card you now can act upon the chats of that card.


#### context fill
when your chat goes for too long and fills all available context
one option is to press
- `alt+3`
that will that start a new chat with the summary of previous one.
