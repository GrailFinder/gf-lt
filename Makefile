.PHONY: setconfig run lint

run: setconfig
	go build -o elefant && ./elefant

server: setconfig
	go build -o elefant && ./elefant -port 3333

setconfig:
	find config.toml &>/dev/null || cp config.example.toml config.toml

lint: ## Run linters. Use make install-linters first.
	golangci-lint run -c .golangci.yml ./...
