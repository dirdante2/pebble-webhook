#!/bin/bash
# Baut das Binary und rollt es über install.sh auf den eigenen Server aus.
#   deploy/deploy.sh [major|minor|fix] [ssh-ziel]   (Vorgabe Ziel: $PEBBLE_DEPLOY_HOST oder SSH-Alias pebble-webhook)
# Version Major.Minor.Fix steht in VERSION; mit major/minor/fix wird sie hochgezählt und nach erfolgreichem
# Ausrollen dort gespeichert. Jede Version braucht einen Abschnitt „## [X.Y.Z]“ in CHANGELOG.md.
#   major (X): brechend für DB/App – neue DB nötig oder nur schwer zurückzuführen (z. B. Migrationen, die Spalten ändern)
#   minor (Y): neue Funktion (z. B. Termine, Mehrsprachigkeit, Theme-Auswahl mit Tag/Nacht)
#   fix   (Z): alle übrigen UI-Änderungen und kleine Bugfixes, beliebig ein- und auszuspielen
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
STUFE=""
case "${1:-}" in major|minor|fix) STUFE="$1"; shift ;; esac
ZIEL="${1:-${PEBBLE_DEPLOY_HOST:-pebble-webhook}}"
IFS=. read -r MAJOR MINOR FIX < VERSION
case "$STUFE" in
    major) MAJOR=$((MAJOR + 1)); MINOR=0; FIX=0 ;;
    minor) MINOR=$((MINOR + 1)); FIX=0 ;;
    fix) FIX=$((FIX + 1)) ;;
esac
SEMVER="$MAJOR.$MINOR.$FIX"
grep -q "^## \[$SEMVER\]" CHANGELOG.md || { echo "CHANGELOG.md: Abschnitt „## [$SEMVER]“ fehlt" >&2; exit 1; }
VERSION="$SEMVER (Build $(date +%Y%m%d-%H%M), $(git describe --always --dirty 2>/dev/null || echo unbekannt))"

go vet ./...
go test ./...
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X 'main.version=$VERSION'" -o bin/pebble-webhook ./cmd/pebble-webhook

scp -q bin/pebble-webhook install.sh "$ZIEL:/tmp/"
ssh "$ZIEL" 'bash /tmp/install.sh --binary /tmp/pebble-webhook && rm -f /tmp/pebble-webhook /tmp/install.sh && curl -sf http://127.0.0.1:8080/healthz'
echo "$SEMVER" > VERSION
echo "Ausgerollt: $VERSION"
