#!/usr/bin/env bash
# Copies the newest server database backup to the Mac. Backups contain health
# and activity data: the target directory is mode 700, files are 600.
#
# With AGE_RECIPIENT set on the server the backups are encrypted ("*.db.age"):
# the server holds only the public key. This script then decrypts the copy on
# the Mac with the private key in AGE_IDENTITY (default
# ~/.config/life-agent/age-key.txt, made by `age-keygen -o`; install age with
# `brew install age`). The encrypted file is kept next to the plain one.
set -euo pipefail

HOST=${DEPLOY_HOST:-cv-prod}
DEST=${BACKUP_DEST:-$HOME/life-agent-backups}
KEEP_DAYS=${KEEP_DAYS:-60}
IDENTITY=${AGE_IDENTITY:-$HOME/.config/life-agent/age-key.txt}

umask 077
mkdir -p "$DEST"
chmod 700 "$DEST"

latest=$(ssh "$HOST" 'ls -1t /opt/life-agent/backup/life-agent-*.db /opt/life-agent/backup/life-agent-*.db.age 2>/dev/null | head -1')
if [ -z "$latest" ]; then
  echo "no backups on $HOST" >&2
  exit 1
fi

name=$(basename "$latest")
scp -q "$HOST:$latest" "$DEST/"
chmod 600 "$DEST/$name"

result="$DEST/$name"
if [[ "$name" == *.age ]]; then
  if ! command -v age >/dev/null 2>&1; then
    echo "age is not installed (brew install age); the encrypted copy is at $result" >&2
    exit 0
  fi
  if [ ! -f "$IDENTITY" ]; then
    echo "no private key at $IDENTITY; the encrypted copy is at $result" >&2
    exit 0
  fi
  plain="${result%.age}"
  age --decrypt --identity "$IDENTITY" --output "$plain" "$result"
  chmod 600 "$plain"
  result="$plain"
fi

find "$DEST" \( -name 'life-agent-*.db' -o -name 'life-agent-*.db.age' \) -mtime +"$KEEP_DAYS" -delete
echo "$result"
