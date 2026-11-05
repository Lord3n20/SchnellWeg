#!/bin/bash
# Baut die Release-APK und legt sie fürs Selbstupdate auf den Server (D-42). Vorher in app/pubspec.yaml die Zahl nach
# dem + erhöhen. Aufruf: SCHNELLWEG_SSH="ssh -i <schlüssel> <nutzer>@<host>" SCHNELLWEG_APP_DIR=<ordner> scripts/app-release.sh
# Die App sieht die neue Fassung erst, wenn version.json geschrieben ist; die vorige APK bleibt zum Zurückrollen liegen.
set -euo pipefail
ssh_cmd=${SCHNELLWEG_SSH:?SCHNELLWEG_SSH fehlt}
ziel=${SCHNELLWEG_APP_DIR:?SCHNELLWEG_APP_DIR fehlt}
flutter=${FLUTTER:-flutter}
cd "$(dirname "$0")/.."

zeile=$(sed -n 's/^version: //p' app/pubspec.yaml)
version=${zeile%+*} code=${zeile#*+}
alt=$($ssh_cmd "sudo cat '$ziel/version.json' 2>/dev/null" | sed -n 's/.*"code": *\([0-9]*\).*/\1/p' || true)
if [ -n "$alt" ] && [ "$code" -le "$alt" ]; then
  echo "Code $code ist nicht größer als der auf dem Server ($alt): app/pubspec.yaml erhöhen"; exit 1
fi

(cd app && "$flutter" test && "$flutter" build apk --release --target-platform android-arm64)
apk=app/build/app/outputs/flutter-apk/app-release.apk
scripts/check-apk.sh "$apk"
sha=$(sha256sum "$apk" | cut -d' ' -f1)

$ssh_cmd "sudo mkdir -p '$ziel' && sudo tee '$ziel/schnellweg-$code.apk.neu' >/dev/null" < "$apk"
fern=$($ssh_cmd "sudo sha256sum '$ziel/schnellweg-$code.apk.neu'" | cut -d' ' -f1)
[ "$fern" = "$sha" ] || { echo "Prüfsumme auf dem Server falsch"; exit 1; }
$ssh_cmd "sudo mv '$ziel/schnellweg-$code.apk.neu' '$ziel/schnellweg-$code.apk'"
printf '{"version":"%s","code":%s,"sha256":"%s"}\n' "$version" "$code" "$sha" |
  $ssh_cmd "sudo tee '$ziel/version.json.neu' >/dev/null && sudo mv '$ziel/version.json.neu' '$ziel/version.json'"
$ssh_cmd "cd '$ziel' && ls -t schnellweg-*.apk | tail -n +3 | sudo xargs -r rm --"
echo "Bereit: $version (Code $code), $sha"
