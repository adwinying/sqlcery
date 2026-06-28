#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cast_path="$repo_root/docs/assets/sqlcery-demo.cast"
demo_root="$(mktemp -d "${TMPDIR:-/tmp}/sqlcery-demo.XXXXXX")"
session="sqlcery-demo-$$"
recording_pid=""

cleanup() {
	if [[ -n "$recording_pid" ]] && kill -0 "$recording_pid" 2>/dev/null; then
		kill "$recording_pid" 2>/dev/null || true
		wait "$recording_pid" 2>/dev/null || true
	fi
	tmux kill-session -t "$session" 2>/dev/null || true
	rm -rf "$demo_root"
}
trap cleanup EXIT INT TERM

wait_for_screen() {
	local expected="$1"
	local attempts=0
	while (( attempts < 100 )); do
		if tmux capture-pane -p -t "$session" 2>/dev/null | grep -Fq "$expected"; then
			return 0
		fi
		sleep 0.1
		attempts=$((attempts + 1))
	done
	printf 'Timed out waiting for demo screen text: %s\n' "$expected" >&2
	tmux capture-pane -p -t "$session" >&2 || true
	return 1
}

wait_for_recorder() {
	local attempts=0
	while (( attempts < 50 )); do
		if ! kill -0 "$recording_pid" 2>/dev/null; then
			wait "$recording_pid" || true
			printf 'Demo recorder exited before attaching to tmux.\n' >&2
			return 1
		fi
		if tmux list-clients -t "$session" -F '#{client_name}' 2>/dev/null | grep -q .; then
			return 0
		fi
		sleep 0.1
		attempts=$((attempts + 1))
	done
	printf 'Timed out waiting for the demo recorder to attach to tmux.\n' >&2
	return 1
}

type_text() {
	local value="$1"
	local index character
	for ((index = 0; index < ${#value}; index++)); do
		character="${value:index:1}"
		tmux send-keys -t "$session" -l "$character"
		sleep 0.045
	done
}

mkdir -p "$demo_root/config/sqlcery" "$demo_root/data" "$demo_root/project"
cp "$repo_root/scripts/demo/global-connections.toml" "$demo_root/config/sqlcery/connections.toml"
cp "$repo_root/scripts/demo/project-connections.toml" "$demo_root/project/connections.toml"

cd "$repo_root"
go run ./scripts/demo/seed.go "$demo_root/project/shop.db"
go build -ldflags "-X main.version=demo -X main.commit=features" -o "$demo_root/sqlcery" ./cmd/sqlcery

printf -v app_command 'cd %q && XDG_CONFIG_HOME=%q XDG_DATA_HOME=%q TERM=xterm-256color %q' \
	"$demo_root/project" "$demo_root/config" "$demo_root/data" "$demo_root/sqlcery"
tmux new-session -d -s "$session" -x 110 -y 34 "$app_command"
tmux set-option -t "$session" status off
wait_for_screen "Connection Picker"

TERM=xterm-256color asciinema record \
	--quiet \
	--headless \
	--overwrite \
	--window-size 110x34 \
	--idle-time-limit 0.8 \
	--title "SQLcery feature tour" \
	--command "tmux attach-session -t $session" \
	"$cast_path" &
recording_pid=$!
wait_for_recorder

sleep 1
type_text "local"
sleep 0.8
tmux send-keys -t "$session" Enter
wait_for_screen "Commands"
sleep 1

type_text "/select orders"
sleep 0.8
tmux send-keys -t "$session" Escape
sleep 0.4
tmux send-keys -t "$session" Enter
wait_for_screen 'FROM "orders";'
sleep 1
tmux send-keys -t "$session" Enter
wait_for_screen "Query returned 5 rows."
sleep 1

tmux send-keys -t "$session" C-x
sleep 0.7
tmux send-keys -t "$session" j
sleep 0.5
tmux send-keys -t "$session" c
sleep 0.35
tmux send-keys -t "$session" c
wait_for_screen 'UPDATE "orders"'
sleep 2.5

kill -INT "$recording_pid" 2>/dev/null || true
wait "$recording_pid" || true
recording_pid=""

# Stopping the attached tmux client emits a terminal teardown frame. Keep the
# final in-app UPDATE frame as the end of the cast instead.
teardown_line="$(grep -nF '\u001b[?1049l' "$cast_path" | tail -n 1 | cut -d: -f1 || true)"
if [[ -n "$teardown_line" ]]; then
	sed -n "1,$((teardown_line - 1))p" "$cast_path" > "$cast_path.tmp"
	mv "$cast_path.tmp" "$cast_path"
fi
