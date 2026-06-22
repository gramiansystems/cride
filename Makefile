GO ?= go
BINARY ?= cride
CMD ?= ./cmd/cride

.DEFAULT_GOAL := build

.PHONY: build clean docs fmt help install run test tidy vet

help:
	@printf 'Available targets:\n'
	@printf '  build  Build the %s binary\n' '$(BINARY)'
	@printf '  install  Install the %s binary with go install\n' '$(BINARY)'
	@printf '  run    Run the app with go run\n'
	@printf '  test   Run all tests\n'
	@printf '  vet    Run go vet\n'
	@printf '  fmt    Format Go source files\n'
	@printf '  docs   Regenerate docs/keymap.md from internal/keymap\n'
	@printf '  tidy   Tidy Go module dependencies\n'
	@printf '  clean  Remove build artifacts\n'

docs:
	$(GO) generate ./internal/keymap

build:
	$(GO) build -o $(BINARY) $(CMD)

install:
	$(GO) install $(CMD)

run:
	$(GO) run $(CMD)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BINARY)
