#!/usr/bin/env bash
# Deploys life-agent to the cv-prod server. Run from the Mac.
#   deploy.sh bootstrap   one-time: user, directories, systemd units, server .env
#   deploy.sh env         rewrite the server .env from the local one, restart the service
#   deploy.sh release     build linux binary, install it, restart the service
#   deploy.sh nginx       one-time: nginx site + Let's Encrypt certificate
#   deploy.sh app         build the phone APK, hand it out as an update
# Nothing outside /opt/life-agent, the life-agent systemd units and the
# life-agent nginx site is touched.
set -euo pipefail

HOST=${DEPLOY_HOST:-cv-prod}
DOMAIN=${DEPLOY_DOMAIN:-life.144-31-165-229.sslip.io}
REMOTE=/opt/life-agent
DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(dirname "$DIR")

# Only these keys go to the server; extend as later stages need more.
SERVER_ENV_KEYS='ACCESS_TOKEN|TIMEZONE|CALENDAR_IDS|ICS_URL|OPENROUTER_API_KEY|CALENDAR_WRITE_ID|STEAM_API_KEY|STEAM_ID|GITHUB_TOKEN|GITHUB_LOGIN|LEETCODE_USER|AGE_RECIPIENT'
SA_KEY_LOCAL="$ROOT/secrets/sa-key.json"

build() {
  (cd "$ROOT/server" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags='-s -w' -o bin/life-agent-linux ./cmd/server)
}

# The server .env: only the whitelisted keys, written with a tight umask.
push_env() {
  { grep -E "^($SERVER_ENV_KEYS)=" "$ROOT/.env"; echo "GOOGLE_SA_KEY_FILE=$REMOTE/secrets/sa-key.json"; } | ssh "$HOST" "set -e
    umask 077
    cat > $REMOTE/.env
    chown lifeagent:lifeagent $REMOTE/.env
    chmod 600 $REMOTE/.env"
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
  push_env
  ssh "$HOST" "set -e
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

# The phone asks GET /v1/app/latest and installs a newer version code over
# itself; that works only with the same signing key (app/android/key.properties).
app_release() {
  local props="$ROOT/app/android/key.properties" pubspec="$ROOT/app/pubspec.yaml"
  [ -f "$props" ] || { echo "no app/android/key.properties: the APK would be signed with another key" >&2; exit 1; }
  local signer
  signer=$(ls -d "${ANDROID_HOME:-$HOME/Library/Android/sdk}"/build-tools/*/apksigner \
    /opt/homebrew/share/android-commandlinetools/build-tools/*/apksigner 2>/dev/null | sort -V | tail -1 || true)
  [ -n "$signer" ] || { echo "apksigner not found" >&2; exit 1; }

  # The version code must grow, or Android refuses the APK as a downgrade.
  local name local_code server_code code
  name=$(sed -nE 's/^version: ([0-9.]+)\+[0-9]+$/\1/p' "$pubspec")
  local_code=$(sed -nE 's/^version: [0-9.]+\+([0-9]+)$/\1/p' "$pubspec")
  [ -n "$name" ] && [ -n "$local_code" ] || { echo "pubspec.yaml: version is not x.y.z+N" >&2; exit 1; }
  server_code=$(ssh "$HOST" "cat $REMOTE/app/latest.json 2>/dev/null || true" | sed -nE 's/.*"version_code": *([0-9]+).*/\1/p')
  server_code=${server_code:-0}
  code=$(( (server_code > local_code ? server_code : local_code) + 1 ))
  sed -i '' -E "s/^version: [0-9.]+\+[0-9]+$/version: $name+$code/" "$pubspec"
  echo "building $name+$code"

  # S21+ is arm64: one ABI keeps the download a third of the full APK.
  (cd "$ROOT/app" && flutter build apk --release --target-platform android-arm64)
  local apk="$ROOT/app/build/app/outputs/flutter-apk/app-release.apk"

  local want got
  # The password goes through the environment, not the command line.
  want=$(KS_PASS=$(sed -nE 's/^storePassword=(.*)$/\1/p' "$props") keytool -exportcert -rfc \
    -keystore "$(sed -nE 's/^storeFile=(.*)$/\1/p' "$props")" -alias "$(sed -nE 's/^keyAlias=(.*)$/\1/p' "$props")" \
    -storepass:env KS_PASS | openssl x509 -outform der | shasum -a 256 | cut -d' ' -f1)
  got=$("$signer" verify --print-certs "$apk" | sed -nE 's/^Signer #1 certificate SHA-256 digest: ([0-9a-f]+)$/\1/p')
  [ "$want" = "$got" ] || { echo "the APK is not signed with the release key" >&2; exit 1; }

  local sum size
  sum=$(shasum -a 256 "$apk" | cut -d' ' -f1)
  size=$(stat -f%z "$apk")
  # The APK first, its description last: until both are in place the sizes
  # differ and the server says there is no release.
  ssh "$HOST" "install -d -o root -g lifeagent -m 750 $REMOTE/app"
  scp "$apk" "$HOST:$REMOTE/app/life-agent.apk.new"
  printf '{"version_code":%d,"version_name":"%s","sha256":"%s","size":%d}\n' "$code" "$name" "$sum" "$size" |
    ssh "$HOST" "set -e
      cat > $REMOTE/app/latest.json.new
      chown root:lifeagent $REMOTE/app/life-agent.apk.new $REMOTE/app/latest.json.new
      chmod 640 $REMOTE/app/life-agent.apk.new $REMOTE/app/latest.json.new
      mv $REMOTE/app/life-agent.apk.new $REMOTE/app/life-agent.apk
      mv $REMOTE/app/latest.json.new $REMOTE/app/latest.json
      cat $REMOTE/app/latest.json"
  echo "commit app/pubspec.yaml: the version code is now $code"
}

case "${1:-}" in
  bootstrap) bootstrap ;;
  env) push_env; ssh "$HOST" "systemctl restart life-agent.service; sleep 1; systemctl is-active life-agent.service" ;;
  release) release ;;
  nginx) nginx_site ;;
  app) app_release ;;
  *) echo "usage: $0 bootstrap|env|release|nginx|app" >&2; exit 2 ;;
esac
