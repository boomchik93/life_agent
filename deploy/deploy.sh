#!/usr/bin/env bash
# Deploys life-agent to the cv-prod server. Run from the Mac.
#   deploy.sh bootstrap   one-time: user, directories, systemd units, server .env
#   deploy.sh release     build linux binary, install it, restart the service
#   deploy.sh nginx       one-time: nginx site + Let's Encrypt certificate
# Nothing outside /opt/life-agent, the life-agent systemd units and the
# life-agent nginx site is touched.
set -euo pipefail

HOST=${DEPLOY_HOST:-cv-prod}
DOMAIN=${DEPLOY_DOMAIN:-life.144-31-165-229.sslip.io}
REMOTE=/opt/life-agent
DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(dirname "$DIR")

# Only these keys go to the server; extend as later stages need more.
SERVER_ENV_KEYS='ACCESS_TOKEN|TIMEZONE|CALENDAR_IDS|ICS_URL'
SA_KEY_LOCAL="$ROOT/secrets/sa-key.json"

build() {
  (cd "$ROOT/server" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags='-s -w' -o bin/life-agent-linux ./cmd/server)
}

bootstrap() {
  ssh "$HOST" "set -e
    id lifeagent >/dev/null 2>&1 || useradd --system --home-dir $REMOTE --no-create-home --shell /usr/sbin/nologin lifeagent
    install -d -o root -g root -m 755 $REMOTE
    install -d -o lifeagent -g lifeagent -m 700 $REMOTE/data $REMOTE/backup $REMOTE/secrets"
  scp "$DIR"/life-agent.service "$DIR"/life-agent-backup.service "$DIR"/life-agent-backup.timer "$HOST":/etc/systemd/system/
  # Service-account key: written through ssh with a tight umask, never left in /tmp.
  ssh "$HOST" "set -e
    umask 077
    cat > $REMOTE/secrets/sa-key.json
    chown lifeagent:lifeagent $REMOTE/secrets/sa-key.json
    chmod 600 $REMOTE/secrets/sa-key.json" < "$SA_KEY_LOCAL"
  { grep -E "^($SERVER_ENV_KEYS)=" "$ROOT/.env"; echo "GOOGLE_SA_KEY_FILE=$REMOTE/secrets/sa-key.json"; } | ssh "$HOST" "set -e
    umask 077
    cat > $REMOTE/.env
    chown lifeagent:lifeagent $REMOTE/.env
    chmod 600 $REMOTE/.env
    systemctl daemon-reload"
}

release() {
  build
  scp "$ROOT/server/bin/life-agent-linux" "$HOST:$REMOTE/life-agent.new"
  ssh "$HOST" "set -e
    chown root:root $REMOTE/life-agent.new
    chmod 755 $REMOTE/life-agent.new
    mv $REMOTE/life-agent.new $REMOTE/life-agent
    systemctl enable life-agent.service life-agent-backup.timer
    systemctl restart life-agent.service
    systemctl start life-agent-backup.timer
    sleep 1
    systemctl is-active life-agent.service"
}

nginx_site() {
  scp "$DIR/nginx-life-agent.conf" "$HOST:/etc/nginx/sites-available/life-agent"
  # A config that fails `nginx -t` must not stay enabled: the next reload
  # (certbot renewal of the other sites) would fail for every site.
  ssh "$HOST" "set -e
    ln -sf /etc/nginx/sites-available/life-agent /etc/nginx/sites-enabled/life-agent
    if ! nginx -t; then rm -f /etc/nginx/sites-enabled/life-agent; exit 1; fi
    systemctl reload nginx
    certbot --nginx -d $DOMAIN --non-interactive --redirect
    nginx -t"
}

case "${1:-}" in
  bootstrap) bootstrap ;;
  release) release ;;
  nginx) nginx_site ;;
  *) echo "usage: $0 bootstrap|release|nginx" >&2; exit 2 ;;
esac
