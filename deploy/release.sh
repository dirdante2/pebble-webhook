#!/bin/bash
# Startet nach dem Push genau einen Release-Build für die gepushte VERSION – per Tag, GitHub baut dann.
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
git tag "v$V-$CH"
git push -q origin "v$V-$CH"
echo "Build gestartet: v$V-$CH"
