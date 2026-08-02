TAILWIND ?= ./tailwindcss$(if $(filter Windows_NT,$(OS)),.exe,)
CSS_INPUT := static/css/input.css
CSS_OUTPUT := static/css/output.css
BINARY := bin/server$(if $(filter Windows_NT,$(OS)),.exe,)

.PHONY: templ tailwind tailwind-watch vet test build dev

templ:
	templ generate

tailwind:
	$(TAILWIND) -i $(CSS_INPUT) -o $(CSS_OUTPUT) --minify

tailwind-watch:
	$(TAILWIND) -i $(CSS_INPUT) -o $(CSS_OUTPUT) --watch

vet:
	go vet ./...

test:
	go test ./...

build: templ tailwind
	go build -o $(BINARY) ./cmd/server

dev: templ tailwind
	go run ./cmd/server
