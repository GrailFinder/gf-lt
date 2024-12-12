.PHONY: setconfig run lint

run: setconfig
	go build -o elefant && ./elefant

setconfig:
	find config.toml &>/dev/null || cp config.example.toml config.toml

lint: ## Run linters. Use make install-linters first.
	golangci-lint run -c .golangci.yml ./...
