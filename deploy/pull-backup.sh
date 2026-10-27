#!/usr/bin/env bash
# Copies the newest server database backup to the Mac. Backups contain health
# data: the target directory is mode 700, files are 600.
set -euo pipefail

HOST=${DEPLOY_HOST:-cv-prod}
DEST=${BACKUP_DEST:-$HOME/life-agent-backups}
KEEP_DAYS=${KEEP_DAYS:-60}

umask 077
mkdir -p "$DEST"
chmod 700 "$DEST"

latest=$(ssh "$HOST" 'ls -1t /opt/life-agent/backup/life-agent-*.db 2>/dev/null | head -1')
if [ -z "$latest" ]; then
  echo "no backups on $HOST" >&2
  exit 1
fi

scp -q "$HOST:$latest" "$DEST/"
chmod 600 "$DEST/$(basename "$latest")"
find "$DEST" -name 'life-agent-*.db' -mtime +"$KEEP_DAYS" -delete
echo "$DEST/$(basename "$latest")"
