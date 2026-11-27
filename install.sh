#!/bin/bash
# pebble-webhook installer and updater for Debian/Ubuntu with systemd.
#
#   curl -fsSL https://raw.githubusercontent.com/dirdante2/pebble-webhook/main/install.sh | sudo bash -s -- --channel stable
#
# Options:
#   --version X.Y.Z     install this release instead of the latest stable one (also X.Y.Z-stable, X.Y.Z-beta,
#                       X.Y.Z-nightly.N, or an older version to roll back; the database is backed up first)
#   --channel NAME      on an installed server: the newest release of stable, beta or nightly (moving
#                       release of that name). A new installation always starts with stable; the channel is
#                       changed afterwards in the web app.
#   --repo OWNER/NAME   GitHub repository to download from (default below)
#   --signing-key FILE  public key (PEM) the release must be signed with, instead of the one below (forks)
#   --source            build from the source tree this script lives in (needs Go)
#   --binary PATH       install a prebuilt binary (used by deploy/deploy.sh)
#   --apply-request     run by pebble-webhook-update.service: install the version requested in the web app,
#                       or restore a backup from BACKUP_DIR (data, settings and the version it was made with)
#
# Running it again updates the program; data, settings and the env file stay untouched.
set -euo pipefail

REPO="dirdante2/pebble-webhook"
REPO_SET=""
# Releases are signed (Ed25519 over SHA256SUMS, deploy/release.sh); only this key counts. A fork signs with its own
# key and puts its public key here (or passes --signing-key FILE).
SIGNING_KEY='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAp3ldhpKRc+ydaA+039LFVX3/EKRWa/QjGVXPA/WPuYI=
-----END PUBLIC KEY-----'
SIGNING_KEY_FILE=""
VERSION=""
CHANNEL=""
MODE="release"
BINARY=""
DATA_DIR="/var/lib/pebble-webhook"
LIB_DIR="/usr/local/lib/pebble-webhook"
REQUEST="$DATA_DIR/update-request"
STATUS="$DATA_DIR/update-status"

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="${2#v}"; shift 2 ;;
        --channel) CHANNEL="$2"; shift 2
            [[ "$CHANNEL" =~ ^(stable|beta|nightly)$ ]] || { echo "--channel must be stable, beta or nightly" >&2; exit 2; } ;;
        --signing-key) SIGNING_KEY_FILE="$2"; shift 2 ;;
        --repo) REPO="$2"; REPO_SET=1; shift 2
            [[ "$REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo "--repo must be OWNER/NAME" >&2; exit 2; } ;;
        --source) MODE="source"; shift ;;
        --binary) MODE="binary"; BINARY="$2"; shift 2 ;;
        --apply-request) MODE="request"; shift ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
done

[ "$(id -u)" = 0 ] || { echo "please run as root (sudo)" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }

# Start values from the env file (defaults for a fresh install).
envval() { local v; v="$(grep -oP "^$1=\K.*" /etc/pebble-webhook/env 2>/dev/null | tail -n1 || true)"; v="${v%\"}"; v="${v#\"}"; echo "${v:-$2}"; }
# Update source: only from the root-owned env file (or --repo here), never from the web app – this script runs as root.
[ -n "$REPO_SET" ] || REPO="$(envval UPDATE_REPO "$REPO")"
# Media (recordings and images, from 4.0 MEDIA_DIR/<id>/audio|bilder); until 3.x the env file named it AUDIO_DIR.
MEDIA_DEFAULT=/srv/pebble-medien
[ -f /etc/pebble-webhook/env ] && [ -d /srv/pebble-audio ] && [ ! -d /srv/pebble-medien ] && MEDIA_DEFAULT=/srv/pebble-audio   # program default until 3.x
MEDIA_DIR="$(envval MEDIA_DIR "$(envval AUDIO_DIR "$MEDIA_DEFAULT")")"
BACKUP_DIR="$(envval BACKUP_DIR /srv/pebble-backup)"
ARCHIVE_RE='^pebble-[0-9A-Za-z.+-]+-(stable|beta|nightly|lokal)-[0-9]{8}-[0-9]{6}-(update|plan|manuell)(-audio)?\.tar\.gz$'
RELEASE_RE='^[0-9]+\.[0-9]+\.[0-9]+(-(stable|beta|nightly)(\.[0-9]+)?)?$'

# Data set: up to 2.x DATA_DIR/pebble.db; from 3.0 DATA_DIR/system.db plus DATA_DIR/users/<id>/daten.db (encrypted,
# keys in /etc/pebble-webhook/keys – never in a backup). Backups, restores and rollbacks always move all of it together.
DB_FILES="pebble.db pebble.db-wal pebble.db-shm system.db system.db-wal system.db-shm"
KEY_DIR=/etc/pebble-webhook/keys
data_present() { [ -f "$1/pebble.db" ] || [ -f "$1/system.db" ]; }
schema_file() { if [ -f "$1/system.db" ]; then echo "$1/system.db"; elif [ -f "$1/pebble.db" ]; then echo "$1/pebble.db"; fi; }
copy_data() {   # $1 → $2 (directories)
    local f
    for f in $DB_FILES; do [ -f "$1/$f" ] && cp -p "$1/$f" "$2/"; done
    # users/ may be a mount point of its own: copy into it, never replace the directory itself.
    if [ -d "$1/users" ]; then install -d -m 0750 "$2/users" && cp -a "$1/users/." "$2/users/"; fi
    return 0
}
replace_data() {   # DATA_DIR gets exactly the data set from $1
    local f
    for f in $DB_FILES; do rm -f "$DATA_DIR/$f"; done
    [ -d "$DATA_DIR/users" ] && find "$DATA_DIR/users" -mindepth 1 -delete
    copy_data "$1" "$DATA_DIR"
    for f in $DB_FILES; do [ -f "$DATA_DIR/$f" ] && [ ! -L "$DATA_DIR/$f" ] && { chown -h pebble:pebble "$DATA_DIR/$f"; chmod 0640 "$DATA_DIR/$f"; }; done
    [ -d "$DATA_DIR/users" ] && chown -R pebble:pebble "$DATA_DIR/users"
    return 0
}

# Files in folders of the service user are written as that user, never as root (no following of planted links).
as_pebble_write() {   # $1 = file, rest = line
    local f="$1"; shift
    runuser -u pebble -- sh -c 'umask 027; printf "%s\n" "$2" > "$1"' _ "$f" "$*"
}
status() {
    if [ -n "${FROM_REQUEST:-}" ]; then as_pebble_write "$STATUS" "$*"; fi
    echo "$*"
}

if [ "$MODE" = request ]; then
    # Written by the web app (user pebble): "<owner/repo> <version>". Validate strictly before use.
    [ -f "$REQUEST" ] || exit 0
    FROM_REQUEST=1
    read -r REQ_REPO REQ_VERSION REQ_EXTRA < "$REQUEST" || true
    rm -f "$REQUEST"
    if [ "$REQ_REPO" = restore ]; then
        # "restore <backup> [<owner/repo>]": an archive from BACKUP_DIR (data + the version it names). The program
        # always comes from the trusted repository (env file); old backup folders with a program (until 1.28) are no
        # longer restored – their program would run as root.
        [[ "$REQ_VERSION" =~ $ARCHIVE_RE ]] || { status "error: invalid backup (old backup folders are no longer supported)"; exit 1; }
        ARCHIVE="$BACKUP_DIR/$REQ_VERSION"
        [ -f "$ARCHIVE" ] && [ ! -L "$ARCHIVE" ] || { status "error: backup not found"; exit 1; }
        MODE="archive"
        # Optional (Ersteinrichtung auf neuer Domain): öffentliche Adresse, die nach dem Einspielen gilt.
        NEW_URL=""
        [[ "${REQ_EXTRA:-}" =~ ^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?$ ]] && NEW_URL="$REQ_EXTRA"
        trap 'status "error: going back to $REQ_VERSION failed (journalctl -u pebble-webhook-update)"' ERR
        status "running restore $REQ_VERSION"
    else
    [[ "$REQ_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { status "error: invalid repository"; exit 1; }
    [ "$REQ_REPO" = "$REPO" ] || { status "error: update source $REQ_REPO not allowed (UPDATE_REPO in /etc/pebble-webhook/env is $REPO)"; exit 1; }
    [[ "$REQ_VERSION" =~ $RELEASE_RE ]] || { status "error: invalid version"; exit 1; }
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
chmod 0711 "$TMP"

# Restoring an archive: unpack it as the service user (never as root), then decide about the program – the version
# named in the backup is installed from its channel; the running one stays if it is that version or if the backup
# comes from an own build ("lokal"), which cannot be downloaded.
DONE_NOTE=""
if [ "$MODE" = archive ]; then
    install -d -o pebble -g pebble -m 0700 "$TMP/restore"
    runuser -u pebble -- tar -xzf "$ARCHIVE" -C "$TMP/restore" --no-same-owner --no-same-permissions
    [ -z "$(find "$TMP/restore" -type l -print -quit)" ] || { status "error: backup contains links"; exit 1; }
    data_present "$TMP/restore" && [ -f "$TMP/restore/meta.json" ] || { status "error: backup incomplete"; exit 1; }
    R_VERSION="$(grep -oP '"version": "\K[^"]+' "$TMP/restore/meta.json" | head -n1 || true)"
    R_CHANNEL="$(grep -oP '"kanal": "\K[^"]+' "$TMP/restore/meta.json" | head -n1 || true)"
    CUR="$(/usr/local/bin/pebble-webhook version 2>/dev/null | awk '{print $1}' || true)"
    RESTORE_DATA=1
    if [ -n "$R_VERSION" ] && [ "$R_VERSION" = "$CUR" ]; then
        MODE="keep"
    elif [[ "$R_CHANNEL" =~ ^(stable|beta|nightly)$ ]] && [[ "$R_VERSION" =~ $RELEASE_RE ]]; then
        MODE="release"; VERSION="$R_VERSION"
    else
        MODE="keep"; DONE_NOTE=" (version ${R_VERSION:-?} cannot be downloaded, kept ${CUR:-the installed one})"
    fi
    echo "Restoring $REQ_VERSION: data from the backup, program ${VERSION:-${CUR:-unchanged}}"
fi

# --- 1. get the program ---
case "$MODE" in
    release)
        # Downloaded releases stay in DATA_DIR/releases/<version> (unused for 30 days → deleted), so switching
        # back and forth between versions does not download them again. The folder belongs to the service user:
        # the checksums always come fresh from the release, cached files only count if they match them.
        command -v curl >/dev/null || { apt-get update -q && apt-get install -yq curl ca-certificates; }
        if [ -n "$VERSION" ]; then BASE="https://github.com/$REPO/releases/download/v$VERSION"
        elif [ -n "$CHANNEL" ]; then BASE="https://github.com/$REPO/releases/download/$CHANNEL"
        else BASE="https://github.com/$REPO/releases/latest/download"; fi
        curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
        # Signature first: everything else is only trusted through SHA256SUMS.
        command -v openssl >/dev/null || { apt-get update -q && apt-get install -yq openssl; }
        if [ -n "$SIGNING_KEY_FILE" ]; then cp "$SIGNING_KEY_FILE" "$TMP/signing.pub"; else printf '%s\n' "$SIGNING_KEY" > "$TMP/signing.pub"; fi
        curl -fsSL -o "$TMP/SHA256SUMS.sig" "$BASE/SHA256SUMS.sig" \
            || { status "error: release ${VERSION:-${CHANNEL:-latest}} is not signed (yet)"; exit 1; }
        openssl pkeyutl -verify -pubin -inkey "$TMP/signing.pub" -rawin -in "$TMP/SHA256SUMS" -sigfile "$TMP/SHA256SUMS.sig" >/dev/null \
            || { status "error: signature of ${VERSION:-${CHANNEL:-latest}} invalid"; exit 1; }
        CACHE=""
        [ -n "$VERSION" ] && CACHE="$DATA_DIR/releases/$VERSION"
        if [ -n "$CACHE" ] && [ -d "$CACHE" ] && [ ! -L "$CACHE" ] \
            && runuser -u pebble -- cat "$CACHE/pebble-webhook-linux-$ARCH" > "$TMP/pebble-webhook-linux-$ARCH" 2>/dev/null \
            && runuser -u pebble -- cat "$CACHE/install.sh" > "$TMP/install.sh" 2>/dev/null \
            && (cd "$TMP" && grep -E " (pebble-webhook-linux-$ARCH|install.sh)\$" SHA256SUMS | sha256sum -c --quiet - >/dev/null 2>&1); then
            echo "Using pebble-webhook $VERSION ($ARCH) from the local cache"
            runuser -u pebble -- touch "$CACHE" || true
        else
            echo "Downloading pebble-webhook ${VERSION:-${CHANNEL:-latest}} ($ARCH) from github.com/$REPO …"
            curl -fsSL -o "$TMP/pebble-webhook-linux-$ARCH" "$BASE/pebble-webhook-linux-$ARCH"
            curl -fsSL -o "$TMP/install.sh" "$BASE/install.sh"
        fi
        (cd "$TMP" && grep -E " (pebble-webhook-linux-$ARCH|install.sh)\$" SHA256SUMS | sha256sum -c --quiet -) \
            || { echo "checksum mismatch" >&2; exit 1; }
        [ "$(grep -cE " (pebble-webhook-linux-$ARCH|install.sh)\$" "$TMP/SHA256SUMS")" = 2 ] || { echo "checksums missing" >&2; exit 1; }
        if [ -n "$CACHE" ] && id pebble >/dev/null 2>&1; then
            # $TMP is root-only: root reads, pebble writes (same as the cache read above).
            if runuser -u pebble -- sh -c 'umask 027; mkdir -p "$1"' _ "$CACHE"; then
                for f in "pebble-webhook-linux-$ARCH" install.sh SHA256SUMS; do
                    runuser -u pebble -- sh -c 'umask 027; cat > "$1"' _ "$CACHE/$f" < "$TMP/$f" || true
                done
            fi
            runuser -u pebble -- find "$DATA_DIR/releases" -mindepth 1 -maxdepth 1 -type d -mtime +30 -exec rm -rf {} + 2>/dev/null || true
        fi
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
    keep)
        install -m 0755 /usr/local/bin/pebble-webhook "$TMP/pebble-webhook"
        ;;
esac
"$TMP/pebble-webhook" version >/dev/null

# --- 1b. database schema: going forward the program runs every migration step in order at start (also across
#         skipped versions); going back only to a version that knows the database's schema (no database changes in
#         between) – otherwise only by restoring a backup made with that version. Programs before the schema
#         number (no "schema" command) count as 0. ---
SCHEMA_DB=""
if [ -n "${RESTORE_DATA:-}" ]; then SCHEMA_DB="$(schema_file "$TMP/restore")"
else SCHEMA_DB="$(schema_file "$DATA_DIR")"; fi
if [ -n "$SCHEMA_DB" ] && id pebble >/dev/null 2>&1; then
    APP_SCHEMA="$("$TMP/pebble-webhook" schema 2>/dev/null || true)"
    [[ "$APP_SCHEMA" =~ ^[0-9]+$ ]] || APP_SCHEMA=0
    DB_SCHEMA=""
    for b in "$TMP/pebble-webhook" /usr/local/bin/pebble-webhook; do
        [ -x "$b" ] || continue
        DB_SCHEMA="$(runuser -u pebble -- "$b" schema "$SCHEMA_DB" 2>/dev/null || true)"
        [[ "$DB_SCHEMA" =~ ^[0-9]+$ ]] && break
        DB_SCHEMA=""
    done
    if [ -n "$DB_SCHEMA" ] && [ "$DB_SCHEMA" -gt "$APP_SCHEMA" ]; then
        status "error: database schema $DB_SCHEMA is newer than ${VERSION:-this version} knows ($APP_SCHEMA) – going back only by restoring a backup"
        exit 1
    fi
    echo "Database schema ${DB_SCHEMA:-?} → program knows $APP_SCHEMA"
fi

# --- 2. user, directories, start values ---
id pebble >/dev/null 2>&1 || useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin pebble
install -d -o pebble -g pebble -m 0750 "$DATA_DIR" "$DATA_DIR/users" "$MEDIA_DIR" "$BACKUP_DIR"
[ -d "$DATA_DIR/releases" ] && chown -R pebble:pebble "$DATA_DIR/releases"
install -d -o root -g pebble -m 0750 /etc/pebble-webhook
install -d -o pebble -g pebble -m 0700 "$KEY_DIR"
install -d -m 0755 "$LIB_DIR"
if [ ! -f /etc/pebble-webhook/env ]; then
    cat > /etc/pebble-webhook/env <<EOF
# pebble-webhook start values. Everything else is configured in the web app (setup wizard, Status → Einstellungen).
LISTEN=0.0.0.0:8080
DATA_DIR=$DATA_DIR
MEDIA_DIR=$MEDIA_DIR
# Backups (database, settings, version, channel; optionally recordings). After changing it run this script again,
# it creates the folder and allows the service to write there.
BACKUP_DIR=$BACKUP_DIR
EOF
    chown root:pebble /etc/pebble-webhook/env
    chmod 0640 /etc/pebble-webhook/env
elif ! grep -q '^BACKUP_DIR=' /etc/pebble-webhook/env; then
    printf '\n# Backups (database, settings, version, channel; optionally recordings). After changing it run install.sh again.\nBACKUP_DIR=%s\n' "$BACKUP_DIR" >> /etc/pebble-webhook/env
fi
# From 4.0 the start value is MEDIA_DIR (recordings and images); an AUDIO_DIR line of 3.x is renamed in place.
if ! grep -q '^MEDIA_DIR=' /etc/pebble-webhook/env; then
    if grep -q '^AUDIO_DIR=' /etc/pebble-webhook/env; then
        sed -i 's/^AUDIO_DIR=/MEDIA_DIR=/' /etc/pebble-webhook/env
    else
        printf '\n# Media: recordings and images per user (MEDIA_DIR/<id>/audio|bilder).\nMEDIA_DIR=%s\n' "$MEDIA_DIR" >> /etc/pebble-webhook/env
    fi
fi
# Update source for the root updater (also shown, locked, in the web app). --repo changes it.
if ! grep -q '^UPDATE_REPO=' /etc/pebble-webhook/env; then
    printf '\n# Update source (GitHub owner/repo) for the updater, which runs as root; "aus" = no updates. Not settable in the web app.\nUPDATE_REPO=%s\n' "$REPO" >> /etc/pebble-webhook/env
elif [ -n "$REPO_SET" ]; then
    sed -i "s|^UPDATE_REPO=.*|UPDATE_REPO=$REPO|" /etc/pebble-webhook/env
fi
# From 4.1 an API key set in the environment always wins over the database and is locked in the app. Old env files
# (before the settings moved into the database) still carry the imported key: comment it out once, the database has it.
if ! grep -q '^# api-keys-from-env' /etc/pebble-webhook/env; then
    sed -i -E 's/^(OPENAI_API_KEY|YOUTUBE_API_KEY)=/# moved to the database (uncomment to fix it from the environment): \1=/' /etc/pebble-webhook/env
    printf '\n# api-keys-from-env: OPENAI_API_KEY / YOUTUBE_API_KEY set here win over the web app and are locked there.\n' >> /etc/pebble-webhook/env
fi

# --- 3. back up the running state before replacing it: an archive in BACKUP_DIR with database (incl. all settings),
#        start values, version and channel (update backups older than 30 days go). The program itself is not
#        backed up – restoring installs the version again; for the health check below a copy stays in $TMP. ---
if data_present "$DATA_DIR" && [ -x /usr/local/bin/pebble-webhook ]; then
    OLD_BUILD="$(/usr/local/bin/pebble-webhook version 2>/dev/null | tr -d '"\\' || true)"
    OLD="$(echo "$OLD_BUILD" | awk '{print $1}')"
    [[ "$OLD" =~ ^[0-9A-Za-z.+-]+$ ]] || OLD="unbekannt"
    OLD_CH="$(cat "$DATA_DIR/installed-channel" 2>/dev/null || true)"
    [[ "$OLD_CH" =~ ^(stable|beta|nightly|lokal)$ ]] || OLD_CH="lokal"
    systemctl stop pebble-webhook 2>/dev/null || true
    cp -p /usr/local/bin/pebble-webhook "$TMP/old-pebble-webhook"
    install -d -m 0700 "$TMP/old"
    copy_data "$DATA_DIR" "$TMP/old"
    FORMAT=1
    [ -f "$TMP/old/system.db" ] && FORMAT=2
    cat > "$TMP/old/meta.json" <<EOF
{
  "format": $FORMAT,
  "version": "$OLD",
  "build": "$OLD_BUILD",
  "kanal": "$OLD_CH",
  "erstellt": "$(date -Iseconds)",
  "art": "update",
  "audio": false,
  "data_dir": "$DATA_DIR",
  "media_dir": "$MEDIA_DIR"
}
EOF
    { echo "# Start values at the time of the backup (/etc/pebble-webhook/env)"
      grep -E '^(LISTEN|DATA_DIR|MEDIA_DIR|BACKUP_DIR)=' /etc/pebble-webhook/env 2>/dev/null || true; } > "$TMP/old/env"
    # API-Schlüssel und Web-Push-Schlüssel nie im Klartext ins Backup (Programme ab 4.3.1 können das).
    if [ -f "$TMP/old/system.db" ] && "$TMP/pebble-webhook" geheimnisse-versiegeln --kann >/dev/null 2>&1; then
        PEBBLE_ENV_FILE=/etc/pebble-webhook/env "$TMP/pebble-webhook" geheimnisse-versiegeln "$TMP/old/system.db" \
            || echo "Warning: secrets in the update backup could not be encrypted" >&2
    fi
    NAME="pebble-$OLD-$OLD_CH-$(date +%Y%m%d-%H%M%S)-update.tar.gz"
    install -d -o pebble -g pebble -m 0750 "$BACKUP_DIR"
    # Read as root, written as the service user (its folder): users/ holds only encrypted files.
    tar -czf - -C "$TMP/old" . | runuser -u pebble -- sh -c 'umask 027; cat > "$1" && mv -fT "$1" "$2"' _ "$BACKUP_DIR/.$NAME.tmp" "$BACKUP_DIR/$NAME"
    BACKUP="$BACKUP_DIR/$NAME"
    find "$BACKUP_DIR" -maxdepth 1 -type f -name 'pebble-*-update*.tar.gz' -mtime +30 -not -path "$BACKUP" -delete
    [ -d "$DATA_DIR/backup" ] && find "$DATA_DIR/backup" -mindepth 1 -maxdepth 1 -name 'pebble-*' -type d -mtime +30 -exec rm -rf {} +
    echo "Backed up to $BACKUP"
fi
if [ -n "${RESTORE_DATA:-}" ]; then
    systemctl stop pebble-webhook 2>/dev/null || true
    replace_data "$TMP/restore"
    install -d -o pebble -g pebble -m 0750 "$MEDIA_DIR"
    chown -R pebble:pebble "$MEDIA_DIR"   # -R follows no links; mounts may belong to root
    # Copied as the service user (unpacked as it, into its folder): links planted in MEDIA_DIR lead nowhere.
    if [ -d "$TMP/restore/medien" ]; then   # from 4.0: medien/<id>/audio|bilder/…
        runuser -u pebble -- cp -R "$TMP/restore/medien/." "$MEDIA_DIR/"
    fi
    if [ -d "$TMP/restore/audio" ]; then    # 3.x: audio/<id>/… → MEDIA_DIR/<id>/audio/…; up to 2.x: audio/<diktat|planung>/…
        for d in "$TMP/restore/audio"/*/; do
            [ -d "$d" ] || continue
            n="$(basename "$d")"
            if [[ "$n" =~ ^[1-9][0-9]*$ ]]; then
                runuser -u pebble -- mkdir -p "$MEDIA_DIR/$n/audio"
                runuser -u pebble -- cp -R "$d." "$MEDIA_DIR/$n/audio/"
            else
                runuser -u pebble -- cp -R "$d" "$MEDIA_DIR/"
            fi
        done
    fi
    echo "Data restored from $ARCHIVE"
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

# Folders (own mounts recommended: DATA_DIR/users, MEDIA_DIR, BACKUP_DIR) often belong to root after creating or
# restoring a mount: set owner and modes now and before every start (programs from 4.0 know the command).
DIRS_PRE=""
if /usr/local/bin/pebble-webhook verzeichnisse --kann >/dev/null 2>&1; then
    PEBBLE_ENV_FILE=/etc/pebble-webhook/env /usr/local/bin/pebble-webhook verzeichnisse
    DIRS_PRE="ExecStartPre=+/usr/local/bin/pebble-webhook verzeichnisse"
fi

cat > /etc/systemd/system/pebble-webhook.service <<EOF
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
$DIRS_PRE
ExecStart=/usr/local/bin/pebble-webhook serve
Restart=always
RestartSec=3
TimeoutStopSec=30
StateDirectory=pebble-webhook
StateDirectoryMode=0750
UMask=0027
ReadWritePaths=-/srv/pebble-audio -/srv/pebble-medien -$MEDIA_DIR -$BACKUP_DIR -$KEY_DIR
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
SystemCallArchitectures=native

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
PrivateTmp=true
ProtectHome=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectClock=true
ProtectHostname=true
RestrictRealtime=true
LockPersonality=true
EOF

systemctl daemon-reload
systemctl enable -q pebble-webhook pebble-webhook-update.path
systemctl restart pebble-webhook-update.path
# Restore onto a new domain: set the public address before the first start (all users then create new passkeys).
if [ -n "${NEW_URL:-}" ] && /usr/local/bin/pebble-webhook adresse --kann >/dev/null 2>&1; then
    systemctl stop pebble-webhook 2>/dev/null || true
    runuser -u pebble -- env PEBBLE_ENV_FILE=/etc/pebble-webhook/env /usr/local/bin/pebble-webhook adresse "$NEW_URL" \
        && echo "Public address set to $NEW_URL" || echo "Warning: public address not set" >&2
fi
# Bring the data set to this version while the service is stopped (e.g. moving to per-user databases), without the
# time limit of the health check. Programs before 2.3 do not know the command.
PREPARED=1
if /usr/local/bin/pebble-webhook vorbereiten --kann >/dev/null 2>&1; then
    systemctl stop pebble-webhook 2>/dev/null || true
    echo "Preparing data for $(/usr/local/bin/pebble-webhook version) …"
    runuser -u pebble -- env PEBBLE_ENV_FILE=/etc/pebble-webhook/env /usr/local/bin/pebble-webhook vorbereiten || PREPARED=""
fi
[ -n "$PREPARED" ] && systemctl restart pebble-webhook

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
if [ -z "$PREPARED" ] || ! healthy; then
    if [ -x "$TMP/old-pebble-webhook" ] && data_present "$TMP/old"; then
        echo "New version not healthy – going back to ${OLD:-the previous version} (program and database as before this run)" >&2
        journalctl -u pebble-webhook -n 20 --no-pager >&2 || true
        systemctl stop pebble-webhook || true
        install -m 0755 "$TMP/old-pebble-webhook" /usr/local/bin/pebble-webhook
        replace_data "$TMP/old"
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
    release) case "$VERSION" in *-beta*) CH=beta ;; *-nightly*) CH=nightly ;; "") CH="${CHANNEL:-stable}" ;; *) CH=stable ;; esac ;;
    keep) if [ -z "$DONE_NOTE" ]; then CH="$R_CHANNEL"; else CH="$(cat "$DATA_DIR/installed-channel" 2>/dev/null || echo lokal)"; fi ;;
    *) CH=lokal ;;
esac
as_pebble_write "$DATA_DIR/installed-channel" "$CH"

NEW="$(/usr/local/bin/pebble-webhook version)"
if [ -n "${FROM_REQUEST:-}" ]; then
    if [ -n "${RESTORE_DATA:-}" ]; then status "done restore $REQ_VERSION$DONE_NOTE"; else status "done $VERSION"; fi
    exit 0
fi
echo "Installed: pebble-webhook $NEW"
if [ "$(curl -fsS http://127.0.0.1:"$(grep -oP '^LISTEN=.*:\K[0-9]+' /etc/pebble-webhook/env || echo 8080)"/healthz 2>/dev/null)" = "ersteinrichtung" ]; then
    PORT_E="$(grep -oP '^LISTEN=.*:\K[0-9]+' /etc/pebble-webhook/env || echo 8080)"
    HOST="$(hostname -I 2>/dev/null | awk '{print $1}')"
    echo
    echo "Not configured yet. Within the next 5 minutes open"
    echo
    echo "  http://${HOST:-<server>}:$PORT_E"
    echo
    echo "in a browser (best through your HTTPS reverse proxy) and choose \"Einrichten\"."
    echo "Too late? Restart the service (systemctl restart pebble-webhook) for another 5 minutes."
fi
