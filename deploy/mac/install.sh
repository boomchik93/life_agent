#!/usr/bin/env bash
# Installs the Mac collector: builds macagent, puts it in ~/.local/bin, makes
# ~/.config/life-agent/mac.env (mode 600) and starts a launchd job that runs
# `macagent sync` every 6 hours. Needs Go and ActivityWatch (aw-server,
# aw-watcher-window, aw-watcher-afk and the browser extension aw-watcher-web).
#
# Nothing is sent from the Mac but categories and minutes: window titles and
# URLs stay in ActivityWatch. Because ActivityWatch keeps them unencrypted on
# disk, turn FileVault on first: System Settings -> Privacy & Security -> FileVault.
set -euo pipefail

DIR=$(cd "$(dirname "$0")/../.." && pwd)
BIN="$HOME/.local/bin"
CONF="$HOME/.config/life-agent"
LABEL=dev.boomchick.life-agent
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

if [ "$(fdesetup status 2>/dev/null | head -1)" != "FileVault is On." ]; then
  echo "FileVault is off. Turn it on before installing: ActivityWatch stores titles and URLs unencrypted." >&2
  exit 1
fi

mkdir -p "$BIN" "$CONF"
chmod 700 "$CONF"
(cd "$DIR/server" && go build -o "$BIN/macagent" ./cmd/macagent)

if [ ! -f "$CONF/mac.env" ]; then
  umask 077
  cat > "$CONF/mac.env" <<'ENV'
LIFE_SERVER=https://
LIFE_TOKEN=
# AW_URL=http://localhost:5600
# TIMEZONE=Asia/Yekaterinburg
ENV
  chmod 600 "$CONF/mac.env"
  echo "Fill in $CONF/mac.env (server address and token), then run this script again."
  exit 0
fi

sed -e "s#__BIN__#$BIN/macagent#g" -e "s#__LOG__#$HOME/Library/Logs/life-agent-mac.log#g" \
  "$DIR/deploy/mac/$LABEL.plist" > "$PLIST"

"$BIN/macagent" check
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
echo "Installed. It runs every 6 hours; run '$BIN/macagent sync' to send now."
