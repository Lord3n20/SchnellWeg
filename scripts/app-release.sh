#!/bin/bash
# Veröffentlicht die App fürs Selbstupdate (D-42). Gebaut wird nicht lokal, sondern von GitHub (Workflow app.yml,
# Tag app-v<version>, signiert, mit Tests und APK-Prüfung). Dieses Skript setzt den Tag, wartet auf den Lauf, lädt die
# APK und legt sie mit version.json auf den Server. Vorher: in app/pubspec.yaml die Zahl nach dem + erhöhen und in
# app/neu.txt kurz schreiben, was das Update ändert (die App zeigt es vor der Installation), committen und pushen.
# Aufruf: SCHNELLWEG_SSH="ssh -i <schlüssel> <nutzer>@<host>" SCHNELLWEG_APP_DIR=<ordner> scripts/app-release.sh
# Braucht nur git, gh (angemeldet), jq und sha256sum. Die vorige APK bleibt zum Zurückrollen liegen.
set -euo pipefail
ssh_cmd=${SCHNELLWEG_SSH:?SCHNELLWEG_SSH fehlt}
ziel=${SCHNELLWEG_APP_DIR:?SCHNELLWEG_APP_DIR fehlt}
cd "$(dirname "$0")/.."

zeile=$(sed -n 's/^version: //p' app/pubspec.yaml)
version=${zeile%+*} code=${zeile#*+} tag=app-v${zeile%+*}
hinweis=$(cat app/neu.txt 2>/dev/null || true)
[ -n "$hinweis" ] || { echo "app/neu.txt fehlt oder ist leer: kurz beschreiben, was das Update ändert"; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "Arbeitsbaum nicht sauber: erst committen"; exit 1; }
git fetch -q origin
[ "$(git rev-parse HEAD)" = "$(git rev-parse '@{u}')" ] || { echo "HEAD ist nicht gepusht"; exit 1; }
alt=$($ssh_cmd "sudo cat '$ziel/version.json' 2>/dev/null" | jq -r '.code // empty' 2>/dev/null || true)
if [ -n "$alt" ] && [ "$code" -le "$alt" ]; then
  echo "Code $code ist nicht größer als der auf dem Server ($alt): app/pubspec.yaml erhöhen"; exit 1
fi

if ! git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null; then
  git tag "$tag" && git push -q origin "$tag"
fi
[ "$(git rev-parse "$tag^{commit}")" = "$(git rev-parse HEAD)" ] || { echo "$tag zeigt nicht auf HEAD"; exit 1; }

echo "Warte auf den Build von $tag auf GitHub …"
lauf=
for _ in $(seq 60); do
  lauf=$(gh run list --workflow app.yml --event push --limit 20 --json databaseId,headBranch \
    --jq ".[] | select(.headBranch == \"$tag\") | .databaseId" | head -1)
  [ -n "$lauf" ] && break
  sleep 5
done
[ -n "$lauf" ] || { echo "Kein Workflow-Lauf für $tag gefunden"; exit 1; }
gh run watch "$lauf" --exit-status --interval 20 >/dev/null || { echo "Build fehlgeschlagen: gh run view $lauf --log-failed"; exit 1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
gh run download "$lauf" -n "schnellweg-$tag" -D "$tmp"
apk=$tmp/app-release.apk
sha=$(sha256sum "$apk" | cut -d' ' -f1)

$ssh_cmd "sudo mkdir -p '$ziel' && sudo tee '$ziel/schnellweg-$code.apk.neu' >/dev/null" < "$apk"
fern=$($ssh_cmd "sudo sha256sum '$ziel/schnellweg-$code.apk.neu'" | cut -d' ' -f1)
[ "$fern" = "$sha" ] || { echo "Prüfsumme auf dem Server falsch"; exit 1; }
$ssh_cmd "sudo mv '$ziel/schnellweg-$code.apk.neu' '$ziel/schnellweg-$code.apk'"
jq -nc --arg v "$version" --argjson c "$code" --arg s "$sha" --arg h "$hinweis" \
  '{version:$v, code:$c, sha256:$s, hinweis:$h}' |
  $ssh_cmd "sudo tee '$ziel/version.json.neu' >/dev/null && sudo mv '$ziel/version.json.neu' '$ziel/version.json'"
$ssh_cmd "cd '$ziel' && ls -t schnellweg-*.apk | tail -n +3 | sudo xargs -r rm --"
echo "Bereit: $version (Code $code), $sha"
