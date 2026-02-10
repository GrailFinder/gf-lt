.PHONY: setconfig run lint setup-whisper build-whisper download-whisper-model docker-up docker-down docker-logs noextra-run noextra-server


run: setconfig
	go build -tags extra -o gf-lt && ./gf-lt

build-debug:
	go build -gcflags="all=-N -l" -tags extra -o gf-lt

debug: build-debug
	dlv exec --headless --accept-multiclient --listen=:2345 ./gf-lt

server: setconfig
	go build -tags extra -o gf-lt && ./gf-lt -port 3333

noextra-run: setconfig
	go build -tags '!extra' -o gf-lt && ./gf-lt

noextra-server: setconfig
	go build -tags '!extra' -o gf-lt && ./gf-lt -port 3333

setconfig:
	find config.toml &>/dev/null || cp config.example.toml config.toml

lint: ## Run linters. Use make install-linters first.
	golangci-lint run -c .golangci.yml ./...

# Whisper STT Setup (in batteries directory)
setup-whisper: build-whisper download-whisper-model

build-whisper: ## Build whisper.cpp from source in batteries directory
	@echo "Building whisper.cpp from source in batteries directory..."
	@if [ ! -d "batteries/whisper.cpp" ]; then \
		echo "Cloning whisper.cpp repository to batteries directory..."; \
		git clone https://github.com/ggml-org/whisper.cpp.git batteries/whisper.cpp; \
	fi
	cd batteries/whisper.cpp && cmake -B build -DGGML_CUDA=ON -DWHISPER_SDL2=ON; cmake --build build --config Release -j 8
	@echo "Whisper binary built successfully!"

download-whisper-model: ## Download Whisper model for STT in batteries directory
	@echo "Downloading Whisper model for STT..."
	@if [ ! -d "batteries/whisper.cpp" ]; then \
		echo "Please run 'make setup-whisper' first to clone the repository."; \
		exit 1; \
	fi
	@cd batteries/whisper.cpp && bash ./models/download-ggml-model.sh large-v3-turbo-q5_0
	@echo "Whisper model downloaded successfully!"

# Docker targets for STT/TTS services (in batteries directory)
docker-up: ## Start all Docker Compose services for STT and TTS from batteries directory
	@echo "Starting Docker services for STT (whisper) and TTS (kokoro)..."
	@echo "Note: The Whisper model will be downloaded automatically inside the container on first run"
	docker-compose -f batteries/docker-compose.yml up -d
	@echo "Docker services started. STT available at http://localhost:8081, TTS available at http://localhost:8880"

docker-up-whisper: ## Start only the Whisper STT service
	@echo "Starting Whisper STT service only..."
	@echo "Note: The Whisper model will be downloaded automatically inside the container on first run"
	docker-compose -f batteries/docker-compose.yml up -d whisper
	@echo "Whisper STT service started. Available at http://localhost:8081"

docker-up-kokoro: ## Start only the Kokoro TTS service
	@echo "Starting Kokoro TTS service only..."
	docker-compose -f batteries/docker-compose.yml up -d kokoro-tts
	@echo "Kokoro TTS service started. Available at http://localhost:8880"

docker-down: ## Stop all Docker Compose services from batteries directory
	@echo "Stopping Docker services..."
	docker-compose -f batteries/docker-compose.yml down
	@echo "Docker services stopped"

docker-down-whisper: ## Stop only the Whisper STT service
	@echo "Stopping Whisper STT service..."
	docker-compose -f batteries/docker-compose.yml down whisper
	@echo "Whisper STT service stopped"

docker-down-kokoro: ## Stop only the Kokoro TTS service
	@echo "Stopping Kokoro TTS service..."
	docker-compose -f batteries/docker-compose.yml down kokoro-tts
	@echo "Kokoro TTS service stopped"

docker-logs: ## View logs from all Docker services in batteries directory
	@echo "Displaying logs from Docker services..."
	docker-compose -f batteries/docker-compose.yml logs -f

docker-logs-whisper: ## View logs from Whisper STT service only
	@echo "Displaying logs from Whisper STT service..."
	docker-compose -f batteries/docker-compose.yml logs -f whisper

docker-logs-kokoro: ## View logs from Kokoro TTS service only
	@echo "Displaying logs from Kokoro TTS service..."
	docker-compose -f batteries/docker-compose.yml logs -f kokoro-tts
