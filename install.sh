#!/bin/bash
# pebble-webhook installer and updater for Debian/Ubuntu with systemd.
#
#   curl -fsSL https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/install.sh | sudo bash
#
# Options:
#   --version X.Y.Z     install this release instead of the latest stable one (also X.Y.Z-beta.N,
#                       X.Y.Z-nightly.N, or an older version to roll back; the database is backed up first)
#   --repo OWNER/NAME   GitHub repository to download from (default below)
#   --source            build from the source tree this script lives in (needs Go)
#   --binary PATH       install a prebuilt binary (used by deploy/deploy.sh)
#   --apply-request     run by pebble-webhook-update.service: install the version requested in the web app,
#                       or go back to a saved state (program and database from DATA_DIR/backup)
#
# Running it again updates the program; data, settings and the env file stay untouched.
set -euo pipefail

REPO="dirdante2/pebble-webhook"
VERSION=""
MODE="release"
BINARY=""
DATA_DIR="/var/lib/pebble-webhook"
LIB_DIR="/usr/local/lib/pebble-webhook"
REQUEST="$DATA_DIR/update-request"
STATUS="$DATA_DIR/update-status"

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="${2#v}"; shift 2 ;;
        --repo) REPO="$2"; shift 2 ;;
        --source) MODE="source"; shift ;;
        --binary) MODE="binary"; BINARY="$2"; shift 2 ;;
        --apply-request) MODE="request"; shift ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
done

[ "$(id -u)" = 0 ] || { echo "please run as root (sudo)" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }

status() {
    if [ -n "${FROM_REQUEST:-}" ]; then echo "$*" > "$STATUS"; chown pebble:pebble "$STATUS"; fi
    echo "$*"
}

if [ "$MODE" = request ]; then
    # Written by the web app (user pebble): "<owner/repo> <version>". Validate strictly before use.
    [ -f "$REQUEST" ] || exit 0
    FROM_REQUEST=1
    read -r REQ_REPO REQ_VERSION < "$REQUEST" || true
    rm -f "$REQUEST"
    if [ "$REQ_REPO" = restore ]; then
        # "restore <backup>": program and database of an earlier state (saved before each update).
        [[ "$REQ_VERSION" =~ ^pebble-[0-9A-Za-z.+-]+-[0-9]{8}-[0-9]{6}$ ]] || { status "error: invalid backup"; exit 1; }
        RESTORE="$DATA_DIR/backup/$REQ_VERSION"
        [ -x "$RESTORE/pebble-webhook" ] && [ -f "$RESTORE/pebble.db" ] || { status "error: backup incomplete"; exit 1; }
        MODE="restore"
        trap 'status "error: going back to $REQ_VERSION failed (journalctl -u pebble-webhook-update)"' ERR
        status "running restore $REQ_VERSION"
    else
    [[ "$REQ_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { status "error: invalid repository"; exit 1; }
    [[ "$REQ_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-(beta|nightly)\.[0-9]+)?$ ]] || { status "error: invalid version"; exit 1; }
    REPO="$REQ_REPO"; VERSION="$REQ_VERSION"; MODE="release"
    trap 'status "error: update to $VERSION failed (journalctl -u pebble-webhook-update)"' ERR
    status "running $VERSION"
    fi
fi

case "$(uname -m)" in
    x86_64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# --- 1. get the program ---
case "$MODE" in
    release)
        # Downloaded releases stay in DATA_DIR/releases/<version> (unused for 30 days → deleted), so switching
        # back and forth between versions does not download them again. Checksums are verified either way.
        CACHE=""
        [ -n "$VERSION" ] && CACHE="$DATA_DIR/releases/$VERSION"
        if [ -n "$CACHE" ] && [ -f "$CACHE/SHA256SUMS" ] && [ -f "$CACHE/pebble-webhook-linux-$ARCH" ] && [ -f "$CACHE/install.sh" ]; then
            echo "Using pebble-webhook $VERSION ($ARCH) from the local cache"
            cp -p "$CACHE/pebble-webhook-linux-$ARCH" "$CACHE/install.sh" "$CACHE/SHA256SUMS" "$TMP/"
            touch "$CACHE"
        else
            command -v curl >/dev/null || { apt-get update -q && apt-get install -yq curl ca-certificates; }
            if [ -n "$VERSION" ]; then BASE="https://github.com/$REPO/releases/download/v$VERSION"
            else BASE="https://github.com/$REPO/releases/latest/download"; fi
            echo "Downloading pebble-webhook ${VERSION:-latest} ($ARCH) from github.com/$REPO …"
            curl -fsSL -o "$TMP/pebble-webhook-linux-$ARCH" "$BASE/pebble-webhook-linux-$ARCH"
            curl -fsSL -o "$TMP/install.sh" "$BASE/install.sh"
            curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
        fi
        (cd "$TMP" && grep -E " (pebble-webhook-linux-$ARCH|install.sh)\$" SHA256SUMS | sha256sum -c --quiet -) \
            || { echo "checksum mismatch" >&2; exit 1; }
        [ "$(grep -cE " (pebble-webhook-linux-$ARCH|install.sh)\$" "$TMP/SHA256SUMS")" = 2 ] || { echo "checksums missing" >&2; exit 1; }
        if [ -n "$CACHE" ] && [ ! -f "$CACHE/SHA256SUMS" ]; then
            install -d -o pebble -g pebble -m 0750 "$DATA_DIR/releases" "$CACHE" 2>/dev/null || install -d -m 0750 "$DATA_DIR/releases" "$CACHE"
            cp -p "$TMP/pebble-webhook-linux-$ARCH" "$TMP/install.sh" "$TMP/SHA256SUMS" "$CACHE/"
        fi
        [ -d "$DATA_DIR/releases" ] && find "$DATA_DIR/releases" -mindepth 1 -maxdepth 1 -type d -mtime +30 -exec rm -rf {} +
        install -m 0755 "$TMP/pebble-webhook-linux-$ARCH" "$TMP/pebble-webhook"
        ;;
    source)
        SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
        command -v go >/dev/null || { echo "Go is required for --source (https://go.dev/dl/)" >&2; exit 1; }
        V="$(cat "$SRC/VERSION" 2>/dev/null || echo dev)"
        echo "Building pebble-webhook $V from $SRC …"
        (cd "$SRC" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X 'main.version=$V'" -o "$TMP/pebble-webhook" ./cmd/pebble-webhook)
        ;;
    binary)
        install -m 0755 "$BINARY" "$TMP/pebble-webhook"
        ;;
    restore)
        # Copy first: the backup of the current state below may rotate the old one away.
        install -m 0755 "$RESTORE/pebble-webhook" "$TMP/pebble-webhook"
        install -d "$TMP/restore-db"
        for f in pebble.db pebble.db-wal pebble.db-shm; do [ -f "$RESTORE/$f" ] && cp -p "$RESTORE/$f" "$TMP/restore-db/"; done
        cp -p "$RESTORE/kanal" "$TMP/kanal" 2>/dev/null || true
        ;;
esac
"$TMP/pebble-webhook" version >/dev/null

# --- 2. user, directories, start values ---
id pebble >/dev/null 2>&1 || useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin pebble
install -d -o pebble -g pebble -m 0750 "$DATA_DIR" "$DATA_DIR/audio"
[ -d "$DATA_DIR/releases" ] && chown -R pebble:pebble "$DATA_DIR/releases"
install -d -o root -g pebble -m 0750 /etc/pebble-webhook
install -d -m 0755 "$LIB_DIR"
if [ ! -f /etc/pebble-webhook/env ]; then
    cat > /etc/pebble-webhook/env <<EOF
# pebble-webhook start values. Everything else is configured in the web app (setup wizard, Status → Einstellungen).
LISTEN=0.0.0.0:8080
DATA_DIR=$DATA_DIR
AUDIO_DIR=$DATA_DIR/audio
EOF
    chown root:pebble /etc/pebble-webhook/env
    chmod 0640 /etc/pebble-webhook/env
fi

# --- 3. back up program and database before replacing a running version (backups older than 30 days go) ---
if [ -f "$DATA_DIR/pebble.db" ] && [ -x /usr/local/bin/pebble-webhook ]; then
    OLD="$(/usr/local/bin/pebble-webhook version 2>/dev/null | awk '{print $1}')"
    BACKUP="$DATA_DIR/backup/pebble-${OLD:-unknown}-$(date +%Y%m%d-%H%M%S)"
    systemctl stop pebble-webhook 2>/dev/null || true
    install -d -o pebble -g pebble -m 0750 "$DATA_DIR/backup" "$BACKUP"
    for f in pebble.db pebble.db-wal pebble.db-shm; do [ -f "$DATA_DIR/$f" ] && cp -p "$DATA_DIR/$f" "$BACKUP/"; done
    # The program too, so the web app can go back to exactly this state (program + database).
    cp -p /usr/local/bin/pebble-webhook "$BACKUP/pebble-webhook"
    # Channel the running version came from (stable/beta/nightly, or "lokal" for own builds).
    [ -f "$DATA_DIR/installed-channel" ] && cp -p "$DATA_DIR/installed-channel" "$BACKUP/kanal"
    chown -R pebble:pebble "$BACKUP"
    find "$DATA_DIR/backup" -mindepth 1 -maxdepth 1 -name 'pebble-*' -type d -mtime +30 -not -path "$BACKUP" -exec rm -rf {} +
    echo "Database backed up to $BACKUP"
fi
if [ "$MODE" = restore ]; then
    systemctl stop pebble-webhook 2>/dev/null || true
    rm -f "$DATA_DIR"/pebble.db "$DATA_DIR"/pebble.db-wal "$DATA_DIR"/pebble.db-shm
    cp -p "$TMP"/restore-db/pebble.db* "$DATA_DIR/"
    chown pebble:pebble "$DATA_DIR"/pebble.db*
    echo "Database restored from $RESTORE"
    VERSION="$REQ_VERSION"
fi

# --- 4. program, CLI wrapper, updater, systemd units ---
install -m 0755 "$TMP/pebble-webhook" /usr/local/bin/pebble-webhook
cat > /usr/local/bin/pebble <<'EOF'
#!/bin/sh
# Runs the CLI as the service user with the service's start values (e.g. `pebble ersteinrichtung`).
exec runuser -u pebble -- env PEBBLE_ENV_FILE=/etc/pebble-webhook/env /usr/local/bin/pebble-webhook "$@"
EOF
chmod 0755 /usr/local/bin/pebble
# The updater is this script: from the release (matches the installed version) or from the local source tree.
if [ -f "$TMP/install.sh" ]; then
    install -m 0755 "$TMP/install.sh" "$LIB_DIR/install.sh"
elif [ -f "${BASH_SOURCE[0]:-/nonexistent}" ] && [ "$(readlink -f "${BASH_SOURCE[0]}")" != "$LIB_DIR/install.sh" ]; then
    install -m 0755 "${BASH_SOURCE[0]}" "$LIB_DIR/install.sh"
fi

cat > /etc/systemd/system/pebble-webhook.service <<'EOF'
[Unit]
Description=pebble-webhook (voice notes from the Pebble Index 01)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=pebble
Group=pebble
EnvironmentFile=/etc/pebble-webhook/env
Environment=PEBBLE_ENV_FILE=/nonexistent
ExecStart=/usr/local/bin/pebble-webhook serve
Restart=always
RestartSec=3
TimeoutStopSec=30
StateDirectory=pebble-webhook
StateDirectoryMode=0750
UMask=0027
ReadWritePaths=-/srv/pebble-audio
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=true

[Install]
WantedBy=multi-user.target
EOF

# Updates from the web app: the app (unprivileged) writes $REQUEST, this path unit starts the root updater.
cat > /etc/systemd/system/pebble-webhook-update.path <<EOF
[Unit]
Description=pebble-webhook: watch for update requests from the web app

[Path]
PathExists=$REQUEST

[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/pebble-webhook-update.service <<EOF
[Unit]
Description=pebble-webhook: install the version requested in the web app

[Service]
Type=oneshot
ExecStart=$LIB_DIR/install.sh --apply-request
EOF

systemctl daemon-reload
systemctl enable -q pebble-webhook pebble-webhook-update.path
systemctl restart pebble-webhook-update.path
systemctl restart pebble-webhook

# --- 5. health check: the service must answer /healthz (database opened and checked). Otherwise go back to the
#        state saved in step 3 – program and database – so a broken update never leaves the app down. ---
PORT="$(grep -oP '^LISTEN=.*:\K[0-9]+' /etc/pebble-webhook/env 2>/dev/null || echo 8080)"
healthy() {
    for _ in $(seq 1 30); do
        sleep 1
        systemctl is-active -q pebble-webhook && curl -fsS -m 3 "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && return 0
    done
    return 1
}
if ! healthy; then
    if [ -n "${BACKUP:-}" ] && [ -x "$BACKUP/pebble-webhook" ] && [ -f "$BACKUP/pebble.db" ]; then
        echo "New version not healthy – going back to ${OLD:-the previous version} (program and database from $BACKUP)" >&2
        journalctl -u pebble-webhook -n 20 --no-pager >&2 || true
        systemctl stop pebble-webhook || true
        install -m 0755 "$BACKUP/pebble-webhook" /usr/local/bin/pebble-webhook
        rm -f "$DATA_DIR"/pebble.db "$DATA_DIR"/pebble.db-wal "$DATA_DIR"/pebble.db-shm
        for f in pebble.db pebble.db-wal pebble.db-shm; do [ -f "$BACKUP/$f" ] && cp -p "$BACKUP/$f" "$DATA_DIR/"; done
        chown pebble:pebble "$DATA_DIR"/pebble.db*
        systemctl start pebble-webhook
        if healthy; then
            status "error: ${VERSION:-new version} did not start – rolled back to ${OLD:-previous version}"
        else
            status "error: ${VERSION:-new version} did not start and the rollback to ${OLD:-previous version} failed (journalctl -u pebble-webhook)"
        fi
    else
        status "error: service did not start (journalctl -u pebble-webhook)"
    fi
    exit 1
fi

# Remember where this version came from (shown with its backup later).
case "$MODE" in
    release) case "$VERSION" in *-beta.*) CH=beta ;; *-nightly.*) CH=nightly ;; "") CH=stable ;; *) CH=stable ;; esac ;;
    restore) CH="$(cat "$TMP/kanal" 2>/dev/null || echo lokal)" ;;
    *) CH=lokal ;;
esac
echo "$CH" > "$DATA_DIR/installed-channel"
chown pebble:pebble "$DATA_DIR/installed-channel"

NEW="$(/usr/local/bin/pebble-webhook version)"
if [ -n "${FROM_REQUEST:-}" ]; then
    status "done $VERSION"
    exit 0
fi
echo "Installed: pebble-webhook $NEW"
if [ "$(curl -fsS http://127.0.0.1:"$(grep -oP '^LISTEN=.*:\K[0-9]+' /etc/pebble-webhook/env || echo 8080)"/healthz 2>/dev/null)" = "ersteinrichtung" ]; then
    LINK="$(/usr/local/bin/pebble ersteinrichtung 2>/dev/null | grep -o 'http://.*' || true)"
    HOST="$(hostname -I 2>/dev/null | awk '{print $1}')"
    echo
    echo "Not configured yet. Open the setup wizard in a browser:"
    echo
    echo "  ${LINK/<server>/${HOST:-<server>}}"
    echo
    echo "Best through your HTTPS reverse proxy (replace http://host:port with your public address)."
fi
