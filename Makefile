.PHONY: setconfig run lint lintall install-linters setup-whisper build-whisper download-whisper-model docker-up docker-down docker-logs noextra-run installdelve checkdelve fetch-onnx install-onnx-deps

run: setconfig
	go build -tags extra -o gf-lt && ./gf-lt

build-debug:
	go build -gcflags="all=-N -l" -tags extra -o gf-lt

debug: build-debug
	dlv exec --headless --accept-multiclient --listen=:2345 ./gf-lt

noextra-run: setconfig
	go build -tags '!extra' -o gf-lt && ./gf-lt

setconfig:
	find config.toml &>/dev/null || cp config.example.toml config.toml

installdelve:
	go install github.com/go-delve/delve/cmd/dlv@latest

checkdelve:
	which dlv &>/dev/null || installdelve

install-linters: ## Install additional linters (noblanks)
	go install github.com/GrailFinder/noblanks-linter/cmd/noblanks@latest

lint: ## Run linters. Use make install-linters first.
	golangci-lint run -c .golangci.yml ./...

lintall: lint
	noblanks ./...

fetch-onnx:
	mkdir -p onnx/embedgemma && curl -o onnx/embedgemma/config.json -L https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/config.json && curl -o onnx/embedgemma/tokenizer.json -L https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/tokenizer.json && curl -o onnx/embedgemma/model_q4.onnx -L https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/onnx/model_q4.onnx && curl -o onnx/embedgemma/model_q4.onnx_data -L https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/onnx/model_q4.onnx_data?download=true

install-onnx-deps: ## Install ONNX Runtime with CUDA support (or CPU fallback)
	@echo "=== ONNX Runtime Installer ===" && \
	echo "" && \
	echo "Checking for existing ONNX Runtime..." && \
	if ldconfig -p 2>/dev/null | grep -q libonnxruntime.so.1; then \
		echo "ONNX Runtime is already installed:" && \
		ldconfig -p 2>/dev/null | grep libonnxruntime && \
		echo "" && \
		echo "Skipping installation. To reinstall, remove existing libs first:" && \
		echo "  sudo rm -f /usr/local/lib/libonnxruntime*.so*" && \
		exit 0; \
	fi && \
	echo "No ONNX Runtime found. Proceeding with installation..." && \
	echo "" && \
	echo "Detecting CUDA version..." && \
	HAS_CUDA=0 && \
	if command -v nvidia-smi >/dev/null 2>&1; then \
		CUDA_INFO=$$(nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -1) && \
		if [ -n "$$CUDA_INFO" ]; then \
			echo "Found NVIDIA GPU with driver: $$CUDA_INFO" && \
			HAS_CUDA=1; \
		else \
			echo "NVIDIA driver found but could not detect CUDA version"; \
		fi; \
	else \
		echo "No NVIDIA GPU detected (nvidia-smi not found)"; \
	fi && \
	echo "" && \
	echo "Determining ONNX Runtime version..." && \
	ARCH=$$(uname -m) && \
	if [ "$$ARCH" = "x86_64" ]; then \
		ONNX_ARCH="x64"; \
	elif [ "$$ARCH" = "aarch64" ] || [ "$$ARCH" = "arm64" ]; then \
		ONNX_ARCH="aarch64"; \
	else \
		echo "Unsupported architecture: $$ARCH" && \
		exit 1; \
	fi && \
	echo "Detected architecture: $$ARCH (ONNX runtime: $$ONNX_ARCH)" && \
	if [ "$$HAS_CUDA" = "1" ]; then \
		echo "Installing ONNX Runtime with CUDA support..."; \
		ONNX_VERSION="1.24.2"; \
	else \
		echo "Installing ONNX Runtime (CPU version)..."; \
		ONNX_VERSION="1.24.2"; \
	fi && \
	FILENAME="onnxruntime-linux-$${ONNX_ARCH}-${ONNX_VERSION}.tgz" && \
	URL="https://github.com/microsoft/onnxruntime/releases/download/v$${ONNX_VERSION}/$${FILENAME}" && \
	echo "Downloading $${URL}..." && \
	mkdir -p /tmp/onnx-install && \
	curl -L -o /tmp/onnx-install/$${FILENAME} "$${URL}" || { \
		echo "Failed to download ONNX Runtime v$${ONNX_VERSION}. Trying v1.18.0..." && \
		ONNX_VERSION="1.18.0" && \
		FILENAME="onnxruntime-linux-$${ONNX_ARCH}-${ONNX_VERSION}.tgz" && \
		URL="https://github.com/microsoft/onnxruntime/releases/download/v$${ONNX_VERSION}/$${FILENAME}" && \
		curl -L -o /tmp/onnx-install/$${FILENAME} "$${URL}" || { \
			echo "ERROR: Failed to download ONNX Runtime from GitHub" && \
			echo "" && \
			echo "Please install manually:" && \
			echo "  1. Go to https://github.com/microsoft/onnxruntime/releases" && \
			echo "  2. Download onnxruntime-linux-$${ONNX_ARCH}-VERSION.tgz" && \
			echo "  3. Extract and copy to /usr/local/lib:" && \
			echo "     tar -xzf onnxruntime-linux-$${ONNX_ARCH}-VERSION.tgz" && \
			echo "     sudo cp -r onnxruntime-linux-$${ONNX_ARCH}-VERSION/lib/* /usr/local/lib/" && \
			echo "     sudo ldconfig" && \
			exit 1; \
		}; \
	} && \
	echo "Extracting..." && \
	cd /tmp/onnx-install && tar -xzf $${FILENAME} && \
	echo "Installing to /usr/local/lib..." && \
	ONNX_DIR=$$(find /tmp/onnx-install -maxdepth 1 -type d -name "onnxruntime-linux-*") && \
	if [ -d "$${ONNX_DIR}/lib" ]; then \
		cp -r $${ONNX_DIR}/lib/* /usr/local/lib/ 2>/dev/null || sudo cp -r $${ONNX_DIR}/lib/* /usr/local/lib/; \
	else \
		echo "ERROR: Could not find lib directory in extracted archive" && \
		exit 1; \
	fi && \
	echo "Updating library cache..." && \
	sudo ldconfig 2>/dev/null || ldconfig && \
	echo "" && \
	echo "=== Installation complete! ===" && \
	echo "" && \
	echo "Installed libraries:" && \
	ldconfig -p | grep libonnxruntime || echo "(libraries may require logout/relogin to appear)" && \
	echo "" && \
	if [ "$$HAS_CUDA" = "1" ]; then \
		echo "NOTE: CUDA-enabled ONNX Runtime installed."; \
		echo "Ensure you also have CUDA libraries installed:"; \
		echo "  - libcudnn, libcublas, libcurand"; \
	else \
		echo "NOTE: CPU-only ONNX Runtime installed."; \
		echo "For GPU support, install CUDA and re-run this script."; \
	fi && \
	rm -rf /tmp/onnx-install

# Whisper STT Setup (in batteries directory)
setup-whisper: build-whisper download-whisper-model

build-whisper: ## Build whisper.cpp from source in batteries directory
	@echo "Building whisper.cpp from source in batteries directory..."
	@if [ ! -f "batteries/whisper.cpp/CMakeLists.txt" ]; then \
		echo "Cloning whisper.cpp repository to batteries directory..."; \
		rm -rf batteries/whisper.cpp; \
		git clone https://github.com/ggml-org/whisper.cpp.git batteries/whisper.cpp; \
	fi
	cd batteries/whisper.cpp && cmake -B build -DGGML_CUDA=ON -DWHISPER_SDL2=ON; cmake --build build --config Release -j 8
	@echo "Whisper binary built successfully!"

download-whisper-model: ## Download Whisper model for STT in batteries directory
	@echo "Downloading Whisper model for STT..."
	@if [ ! -d "batteries/whisper.cpp/models" ]; then \
		mkdir -p "batteries/whisper.cpp/models"; \
	fi
	curl -o batteries/whisper.cpp/models/ggml-large-v3-turbo-q5_0.bin -L "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo-q5_0.bin?download=true"
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
