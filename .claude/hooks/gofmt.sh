#!/usr/bin/env bash
# PostToolUse hook: форматирует Go-файл сразу после записи агентом.
# Claude Code передаёт JSON события в stdin.
set -uo pipefail

payload="$(cat)"
file="$(printf '%s' "$payload" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

[[ "$file" == *.go ]] || exit 0
[[ -f "$file" ]] || exit 0

gofmt -w "$file"
