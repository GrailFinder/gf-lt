### gf-lt (grail finder's llm tui)
Terminal program to chat with llm.

#### Has/Supports
- character card spec;
- llama.cpp api, deepseek (other ones were not tested);
- showing images (not really, for now only if your char card is png it could show it);
- tts/sst (if whisper.cpp server / fastapi tts server are provided);

#### usage examples
[!usage example](assets/ex01.png)

#### how to install
clone the project
```
cd gf-lt
make
```

#### setting up config
```
cp config.example.toml config.toml
```
set values as you need them to be.
