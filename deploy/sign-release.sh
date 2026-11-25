#!/bin/bash
# Signiert Releases auf GitHub: SHA256SUMS jedes genannten Releases mit dem privaten Ed25519-Schlüssel signieren
# und als SHA256SUMS.sig hochladen. install.sh installiert nur Releases mit gültiger Signatur (öffentlicher Schlüssel
# fest in install.sh). Der private Schlüssel liegt nur lokal, nie bei GitHub – ein übernommenes GitHub-Konto kann
# so keine installierbaren Releases veröffentlichen.
#   deploy/sign-release.sh TAG...      z. B. v4.4.0-nightly nightly
#   deploy/sign-release.sh --alle      alle vorhandenen Releases (auch die wandernden stable/beta/nightly)
# Schlüssel: $PEBBLE_SIGNING_KEY, sonst ~/.ssh/pebble-release-signing.pem
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
KEY="${PEBBLE_SIGNING_KEY:-$HOME/.ssh/pebble-release-signing.pem}"
[ -r "$KEY" ] || { echo "Signierschlüssel fehlt: $KEY" >&2; exit 1; }

# Der Schlüssel muss zum öffentlichen in install.sh passen – sonst lehnt jede Installation ab.
PUB_SCRIPT="$(sed -n "/^SIGNING_KEY='/,/END PUBLIC KEY/p" install.sh | sed "s/^SIGNING_KEY='//; s/'\$//")"
[ "$(openssl pkey -in "$KEY" -pubout)" = "$PUB_SCRIPT" ] || { echo "Schlüssel passt nicht zum öffentlichen Schlüssel in install.sh" >&2; exit 1; }

TAGS=("$@")
if [ "${1:-}" = --alle ]; then
    mapfile -t TAGS < <(gh release list --limit 500 --json tagName --jq '.[].tagName')
fi
[ ${#TAGS[@]} -gt 0 ] || { echo "Aufruf: deploy/sign-release.sh TAG... | --alle" >&2; exit 1; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
for tag in "${TAGS[@]}"; do
    rm -f "$T/SHA256SUMS" "$T/SHA256SUMS.sig"
    if ! gh release download "$tag" -p SHA256SUMS -D "$T" 2>/dev/null; then
        echo "$tag: keine SHA256SUMS – übersprungen"
        continue
    fi
    openssl pkeyutl -sign -inkey "$KEY" -rawin -in "$T/SHA256SUMS" -out "$T/SHA256SUMS.sig"
    gh release upload "$tag" "$T/SHA256SUMS.sig" --clobber
    echo "$tag: signiert"
done
