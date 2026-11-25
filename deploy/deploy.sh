#!/bin/bash
# Baut das Binary und rollt es über install.sh auf den eigenen Server aus.
#   deploy/deploy.sh [ssh-ziel]   (Vorgabe Ziel: $PEBBLE_DEPLOY_HOST oder SSH-Alias pebble-webhook)
# Version X.Y.Z.A: X.Y.Z steht in VERSION (in Git), A ist ein lokaler Build-Zähler in lokal/BUILD (nie in Git).
# Ein Deploy ändert nie X.Y.Z, es zählt nur A hoch. X.Y.Z wird erst am Ende eines Arbeitsblocks vor dem Push
# von Hand gesetzt (VERSION + Abschnitt „## [X.Y.Z]“ in CHANGELOG.md); jede neue X.Y.Z beginnt mit A=1.
#   X: brechend für DB/App – neue DB nötig oder nur schwer zurückzuführen (z. B. Migrationen, die Spalten ändern)
#   Y: neue Funktion (z. B. Termine, Mehrsprachigkeit, Theme-Auswahl mit Tag/Nacht)
#   Z: alle übrigen UI-Änderungen und kleine Bugfixes, beliebig ein- und auszuspielen
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
case "${1:-}" in major|minor|fix) echo "Deploy ändert X.Y.Z nicht mehr; Version erst vor dem Push in VERSION setzen" >&2; exit 1 ;; esac
ZIEL="${1:-${PEBBLE_DEPLOY_HOST:-pebble-webhook}}"

XYZ_RE='^[0-9]+\.[0-9]+\.[0-9]+$'
CUR="$(tr -d '[:space:]' < VERSION)"
[[ "$CUR" =~ $XYZ_RE ]] || { echo "VERSION ist nicht X.Y.Z: „$CUR“ (A gehört nach lokal/BUILD)" >&2; exit 1; }

SEMVER="$CUR"

mkdir -p lokal
LETZT="$(cat lokal/BUILD 2>/dev/null | tr -d '[:space:]' || true)"
if [ "${LETZT%.*}" = "$SEMVER" ] && [[ "${LETZT##*.}" =~ ^[0-9]+$ ]]; then A=$(( ${LETZT##*.} + 1 )); else A=1; fi
LOKAL="$SEMVER.$A"

grep -q "^## \[$SEMVER\]" CHANGELOG.md || { echo "CHANGELOG.md: Abschnitt „## [$SEMVER]“ fehlt" >&2; exit 1; }
VERSION="$LOKAL (Build $(date +%Y%m%d-%H%M), $(git describe --always --dirty 2>/dev/null || echo unbekannt))"

deploy/whisper-engine.sh # lokale Transkription; baut nur, wenn sie fehlt oder veraltet ist
go vet ./...
go test ./...
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X 'main.version=$VERSION'" -o bin/pebble-webhook ./cmd/pebble-webhook

# Eigener, nur für root lesbarer Ordner auf dem Ziel (kein festes /tmp/install.sh, das ein anderer vorher anlegen könnte).
RDIR="$(ssh "$ZIEL" 'mktemp -d')"
scp -q bin/pebble-webhook install.sh "$ZIEL:$RDIR/"
ssh "$ZIEL" "bash $RDIR/install.sh --binary $RDIR/pebble-webhook; rc=\$?; rm -rf $RDIR; [ \$rc = 0 ] && curl -sf http://127.0.0.1:8080/healthz"
echo "$SEMVER" > VERSION
echo "$LOKAL" > lokal/BUILD
echo "Ausgerollt: $VERSION"
