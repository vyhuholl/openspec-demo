GO ?= go
PORT ?= 8080
SPEC_LINT ?= 0

# Флаги зависят от версии OpenSpec CLI — проверить `openspec validate --help`.
OPENSPEC_VALIDATE ?= openspec validate --all --strict

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "run            — поднять сервис на :$(PORT)"
	@echo "build          — собрать все пакеты"
	@echo "fmt            — gofmt -w ."
	@echo "fmt-check      — упасть, если что-то не отформатировано"
	@echo "vet            — go vet"
	@echo "lint           — golangci-lint, если установлен"
	@echo "test           — go test -race"
	@echo "coverage       — покрытие по функциям"
	@echo "spec-lint      — валидация спек OpenSpec"
	@echo "ai-check       — полная проверка; SPEC_LINT=1 добавляет spec-lint"

.PHONY: run
run:
	PORT=$(PORT) $(GO) run ./cmd/booking

.PHONY: build
build:
	$(GO) build ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "нужен gofmt:"; echo "$$files"; exit 1; \
	fi

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint не установлен — пропускаю"; \
	fi

.PHONY: test
test:
	$(GO) test -race -count=1 ./...

.PHONY: coverage test-coverage
test-coverage: coverage
coverage:
	$(GO) test -race -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

.PHONY: spec-lint
spec-lint:
	@if [ ! -d openspec ]; then \
		echo "openspec/ нет в этой ветке — пропускаю"; exit 0; \
	fi; \
	if ! command -v openspec >/dev/null 2>&1; then \
		echo "openspec CLI не найден"; exit 1; \
	fi; \
	$(OPENSPEC_VALIDATE)

.PHONY: ai-check
ai-check:
	$(MAKE) fmt-check
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) test
	@if [ "$(SPEC_LINT)" = "1" ]; then $(MAKE) spec-lint; fi