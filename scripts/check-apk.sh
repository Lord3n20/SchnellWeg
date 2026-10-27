#!/bin/bash
# Prüft eine APK auf Google-Dienste, Firebase, Analytics und Tracker (CLAUDE.md: „APK-Prüfung bricht Build ab“).
# Geprüft werden die tatsächlich enthaltenen Klassen (nicht bloße Verweise), Berechtigungen und Manifest-Einträge.
# Aufruf: scripts/check-apk.sh app.apk [mapping.txt]   → Exit 1 bei Fund
set -euo pipefail
apk=${1:?APK angeben}
bt=${ANDROID_HOME:-$HOME/.local/share/android-sdk}/build-tools
bt=$bt/$(ls "$bt" | sort -V | tail -1)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Klassen-Präfixe, die nicht in die App dürfen
verboten='^L(com/google/android/gms|com/google/firebase|com/google/android/play|com/google/android/datatransport|com/google/mlkit|com/google/ads|com/android/installreferrer|com/crashlytics|io/fabric|io/sentry|com/facebook|com/appsflyer|com/adjust|com/onesignal|com/mixpanel|com/amplitude|com/bugsnag|com/microsoft/appcenter|io/branch)/'

unzip -q -o "$apk" 'classes*.dex' -d "$tmp"
fehler=0
for dex in "$tmp"/classes*.dex; do
  "$bt/dexdump" "$dex" | sed -n "s/^  Class descriptor  : '\(.*\)'$/\1/p"
done | sort -u > "$tmp/klassen"
if grep -E "$verboten" "$tmp/klassen" > "$tmp/funde"; then
  echo "VERBOTENE KLASSEN ($(wc -l < "$tmp/funde")):"
  sed 's|/[^/]*;$||' "$tmp/funde" | sort | uniq -c | sort -rn | head -20
  fehler=1
fi

# R8 benennt in Release-Builds die meisten Klassen um (a.b, c.d …); die Originalnamen stehen in der Mapping-Datei.
# Ohne sie wäre die Klassenprüfung blind → Pflicht, sobald die APK verkleinert ist.
mapping=${2:-$(dirname "$apk")/../mapping/release/mapping.txt}
if [ -f "$mapping" ]; then
  sed -n 's/^\([^ #][^ ]*\) -> .*:$/L\1/p' "$mapping" | tr . / | sed 's/$/;/' | sort -u > "$tmp/original"
  if grep -E "$verboten" "$tmp/original" > "$tmp/mfunde0"; then
    echo "VERBOTENE KLASSEN laut R8-Mapping ($(wc -l < "$tmp/mfunde0")):"
    sed 's|/[^/]*;$||' "$tmp/mfunde0" | sort | uniq -c | sort -rn | head -20
    fehler=1
  fi
  echo "Klassen laut Mapping: $(wc -l < "$tmp/original")"
elif [ "$(grep -cE '^L([a-z]{1,2}/)*[A-Za-z0-9]{1,3};$' "$tmp/klassen")" -gt 50 ]; then
  echo "APK ist von R8 verkleinert, aber keine Mapping-Datei gefunden (2. Argument) → Prüfung unvollständig"
  fehler=1
fi

"$bt/aapt2" dump xmltree --file AndroidManifest.xml "$apk" > "$tmp/manifest"
if grep -nE 'com\.google\.(android\.gms|firebase)|AD_ID|c2dm|INSTALL_REFERRER|com\.google\.android\.finsky' "$tmp/manifest" > "$tmp/mfunde"; then
  echo "VERBOTENE MANIFEST-EINTRÄGE:"
  cat "$tmp/mfunde"
  fehler=1
fi

echo "Berechtigungen:"
"$bt/aapt2" dump permissions "$apk" | sed -n 's/^uses-permission: name=.\([^'"'"']*\).*/  \1/p'
echo "Klassen gesamt: $(wc -l < "$tmp/klassen")"
if [ $fehler = 0 ]; then echo "OK: keine Google-Dienste, kein Firebase, keine Tracker"; fi
exit $fehler
