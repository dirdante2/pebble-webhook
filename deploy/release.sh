#!/bin/bash
# Startet nach dem Push genau einen Release-Build für die gepushte VERSION – per Tag, GitHub baut dann; danach
# signiert es das Release lokal (deploy/sign-release.sh). Erst signierte Releases installiert install.sh.
#   deploy/release.sh [nightly|beta|stable]   (Vorgabe nightly)
# Ein Push auf main baut nichts. Je Push ein Kanal: ein stable- oder beta-Build braucht keine nightly.
# beta/stable brauchen einen Abschnitt in RELEASES.md (## [X.Y.Z-beta] bzw. ## [X.Y.Z]).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
CH="${1:-nightly}"
case "$CH" in nightly|beta|stable) ;; *) echo "Kanal: nightly, beta oder stable" >&2; exit 1 ;; esac
V="$(tr -d '[:space:]' < VERSION)"
[[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "VERSION ist nicht X.Y.Z: „$V“" >&2; exit 1; }
git diff --quiet HEAD -- VERSION || { echo "VERSION ist nicht committet" >&2; exit 1; }
git fetch -q origin main # ohne --tags: die wandernden Tags stable/beta/nightly ändern sich auf GitHub
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || { echo "HEAD ist nicht der gepushte Stand von main – erst pushen" >&2; exit 1; }
[ -n "$(git ls-remote --tags origin "refs/tags/v$V-$CH")" ] && { echo "v$V-$CH gibt es schon – für einen neuen Build VERSION anheben" >&2; exit 1; }
case "$CH" in
    stable) grep -q "^## \[$V\]" RELEASES.md || { echo "RELEASES.md: Abschnitt „## [$V]“ fehlt" >&2; exit 1; } ;;
    beta) grep -q "^## \[$V-beta\]" RELEASES.md || { echo "RELEASES.md: Abschnitt „## [$V-beta]“ fehlt" >&2; exit 1; } ;;
esac
KEY="${PEBBLE_SIGNING_KEY:-$HOME/.ssh/pebble-release-signing.pem}"
[ -r "$KEY" ] || { echo "Signierschlüssel fehlt ($KEY) – ohne Signatur installiert kein Server das Release" >&2; exit 1; }
git tag "v$V-$CH"
git push -q origin "v$V-$CH"
echo "Build gestartet: v$V-$CH – warte auf GitHub, dann wird signiert"
# Den Lauf zum Tag finden, abwarten, danach SHA256SUMS lokal signieren (Release und wandernder Kanal-Release).
RUN=""
for _ in $(seq 1 30); do
    RUN="$(gh run list --workflow release.yml --branch "v$V-$CH" --limit 1 --json databaseId --jq '.[0].databaseId // empty')"
    [ -n "$RUN" ] && break
    sleep 5
done
[ -n "$RUN" ] || { echo "Build-Lauf nicht gefunden – später signieren: deploy/sign-release.sh v$V-$CH $CH" >&2; exit 1; }
gh run watch "$RUN" --exit-status >/dev/null || { echo "Build fehlgeschlagen (gh run view $RUN)" >&2; exit 1; }
# Der Kanal-Release muss genau diesen Build tragen (sonst nur die Version signieren).
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
gh release download "v$V-$CH" -p SHA256SUMS -D "$T/v"
gh release download "$CH" -p SHA256SUMS -D "$T/k" 2>/dev/null || true
if cmp -s "$T/v/SHA256SUMS" "$T/k/SHA256SUMS"; then deploy/sign-release.sh "v$V-$CH" "$CH"; else deploy/sign-release.sh "v$V-$CH"; fi
echo "Veröffentlicht und signiert: v$V-$CH"
