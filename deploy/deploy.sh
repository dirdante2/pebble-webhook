#!/bin/bash
# Baut das Binary und rollt es über install.sh auf den eigenen Server aus.
#   deploy/deploy.sh [major|minor|fix] [ssh-ziel]   (Vorgabe Ziel: $PEBBLE_DEPLOY_HOST oder SSH-Alias pebble-webhook)
# Version X.Y.Z.A: X.Y.Z steht in VERSION (in Git), A ist ein lokaler Build-Zähler in lokal/BUILD (nie in Git).
# X.Y.Z springt einmal je Arbeitsblock (zwischen zwei Commits): das erste Deploy nach einem Commit braucht eine
# Stufe; jedes weitere Deploy zählt nur A hoch. Eine höhere Stufe im Block hebt X.Y.Z neu ab dem committeten Stand
# (1.33.11 → 1.34.0). Jede neue X.Y.Z beginnt mit A=1. Jede X.Y.Z braucht einen Abschnitt „## [X.Y.Z]“ in
# CHANGELOG.md; die Änderungen eines Blocks sammeln sich dort.
#   major (X): brechend für DB/App – neue DB nötig oder nur schwer zurückzuführen (z. B. Migrationen, die Spalten ändern)
#   minor (Y): neue Funktion (z. B. Termine, Mehrsprachigkeit, Theme-Auswahl mit Tag/Nacht)
#   fix   (Z): alle übrigen UI-Änderungen und kleine Bugfixes, beliebig ein- und auszuspielen
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
STUFE=""
case "${1:-}" in major|minor|fix) STUFE="$1"; shift ;; esac
ZIEL="${1:-${PEBBLE_DEPLOY_HOST:-pebble-webhook}}"

XYZ_RE='^[0-9]+\.[0-9]+\.[0-9]+$'
BASE="$(git show HEAD:VERSION 2>/dev/null | tr -d '[:space:]')"
CUR="$(tr -d '[:space:]' < VERSION)"
[[ "$BASE" =~ $XYZ_RE ]] || { echo "VERSION im letzten Commit ist nicht X.Y.Z: „$BASE“" >&2; exit 1; }
[[ "$CUR" =~ $XYZ_RE ]] || { echo "VERSION ist nicht X.Y.Z: „$CUR“ (A gehört nach lokal/BUILD)" >&2; exit 1; }

# ver_lt a b: a < b (X.Y.Z)
ver_lt() { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)" = "$1" ]; }

if [ -n "$STUFE" ]; then
    IFS=. read -r MAJOR MINOR FIX <<< "$BASE"
    case "$STUFE" in
        major) MAJOR=$((MAJOR + 1)); MINOR=0; FIX=0 ;;
        minor) MINOR=$((MINOR + 1)); FIX=0 ;;
        fix) FIX=$((FIX + 1)) ;;
    esac
    SEMVER="$MAJOR.$MINOR.$FIX"
    ver_lt "$SEMVER" "$CUR" && SEMVER="$CUR"
elif [ "$CUR" = "$BASE" ]; then
    echo "Neuer Arbeitsblock (VERSION = letzter Commit $BASE): Stufe major|minor|fix angeben" >&2; exit 1
else
    SEMVER="$CUR"
fi

mkdir -p lokal
LETZT="$(cat lokal/BUILD 2>/dev/null | tr -d '[:space:]' || true)"
if [ "${LETZT%.*}" = "$SEMVER" ] && [[ "${LETZT##*.}" =~ ^[0-9]+$ ]]; then A=$(( ${LETZT##*.} + 1 )); else A=1; fi
LOKAL="$SEMVER.$A"

grep -q "^## \[$SEMVER\]" CHANGELOG.md || { echo "CHANGELOG.md: Abschnitt „## [$SEMVER]“ fehlt" >&2; exit 1; }
VERSION="$LOKAL (Build $(date +%Y%m%d-%H%M), $(git describe --always --dirty 2>/dev/null || echo unbekannt))"

go vet ./...
go test ./...
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X 'main.version=$VERSION'" -o bin/pebble-webhook ./cmd/pebble-webhook

scp -q bin/pebble-webhook install.sh "$ZIEL:/tmp/"
ssh "$ZIEL" 'bash /tmp/install.sh --binary /tmp/pebble-webhook && rm -f /tmp/pebble-webhook /tmp/install.sh && curl -sf http://127.0.0.1:8080/healthz'
echo "$SEMVER" > VERSION
echo "$LOKAL" > lokal/BUILD
echo "Ausgerollt: $VERSION"
