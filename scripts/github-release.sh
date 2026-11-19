#!/bin/bash
# Legt das GitHub-Release zum Tag app-v<version> an (falls noch keins da ist) und hängt die Dateien an (D-53).
# Läuft in app.yml (APK) und ios.yml (IPA + quelle.json); wer zuerst fertig ist, legt das Release an.
# Aufruf im Workflow: GH_TOKEN, GH_REPO, GITHUB_REF_NAME gesetzt; scripts/github-release.sh <datei>…
set -euo pipefail
cd "$(dirname "$0")/.."
tag=$GITHUB_REF_NAME
zeile=$(sed -n 's/^version: //p' app/pubspec.yaml)
notiz="$(cat app/neu.txt)

- Android: SchnellWeg.apk herunterladen und installieren (danach kommen Updates über die App).
- iPhone: Anleitung in docs/iphone.md, Quelle für SideStore/AltStore: https://github.com/$GH_REPO/releases/latest/download/quelle.json"
for _ in 1 2 3; do
  gh release view "$tag" >/dev/null 2>&1 && break
  gh release create "$tag" --title "SchnellWeg ${zeile%+*}" --notes "$notiz" && break
  sleep 5 # der andere Workflow hat es vielleicht gerade angelegt
done
gh release upload "$tag" "$@" --clobber
