.PHONY: setconfig run lint setup-whisper build-whisper download-whisper-model docker-up docker-down docker-logs

run: setconfig
	go build -o gf-lt && ./gf-lt

server: setconfig
	go build -o gf-lt && ./gf-lt -port 3333

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
	cd batteries/whisper.cpp && make build
	@echo "Creating symlink to whisper-cli binary..."
	@ln -sf batteries/whisper.cpp/build/bin/whisper-cli ./whisper-cli
	@echo "Whisper binary built successfully!"

download-whisper-model: ## Download Whisper model for STT in batteries directory
	@echo "Downloading Whisper model for STT..."
	@if [ ! -d "batteries/whisper.cpp" ]; then \
		echo "Please run 'make setup-whisper' first to clone the repository."; \
		exit 1; \
	fi
	@cd batteries/whisper.cpp && make tiny.en
	@echo "Creating symlink to Whisper model..."
	@ln -sf batteries/whisper.cpp/models/ggml-tiny.en.bin ./ggml-model.bin
	@echo "Whisper model downloaded successfully!"

# Docker targets for STT/TTS services (in batteries directory)
docker-up: ## Start Docker Compose services for STT and TTS from batteries directory
	@echo "Starting Docker services for STT (whisper) and TTS (kokoro)..."
	docker-compose -f batteries/docker-compose.yml up -d
	@echo "Docker services started. STT available at http://localhost:8081, TTS available at http://localhost:8880"

docker-down: ## Stop Docker Compose services from batteries directory
	@echo "Stopping Docker services..."
	docker-compose -f batteries/docker-compose.yml down
	@echo "Docker services stopped"

docker-logs: ## View logs from Docker services in batteries directory
	@echo "Displaying logs from Docker services..."
	docker-compose -f batteries/docker-compose.yml logs -f

# Convenience target to setup everything
setup-complete: setup-whisper docker-up
	@echo "Complete setup finished! STT and TTS services are running."
