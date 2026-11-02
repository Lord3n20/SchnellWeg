#!/bin/sh
# Erzeugt aus ic_launcher_foreground.svg alle Android-Ressourcen des App-Icons. Braucht rsvg-convert.
set -eu
cd "$(dirname "$0")"
HINTERGRUND='#5A5F66'
RES=../android/app/src/main/res
PFAD=$(sed -n 's/.* d="\([^"]*\)".*/\1/p' ic_launcher_foreground.svg)

mkdir -p "$RES/mipmap-anydpi-v26"
cat > "$RES/mipmap-anydpi-v26/ic_launcher.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground android:drawable="@drawable/ic_launcher_foreground"/>
    <monochrome android:drawable="@drawable/ic_launcher_foreground"/>
</adaptive-icon>
XML
cat > "$RES/values/ic_launcher_background.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<resources>
    <color name="ic_launcher_background">$HINTERGRUND</color>
</resources>
XML
cat > "$RES/drawable/ic_launcher_foreground.xml" <<XML
<vector xmlns:android="http://schemas.android.com/apk/res/android" android:width="108dp" android:height="108dp"
    android:viewportWidth="108" android:viewportHeight="108">
    <path android:fillColor="#FFFFFFFF" android:fillType="evenOdd"
        android:pathData="$PFAD"/>
</vector>
XML

# Klassisches Icon mit eigenen Ecken: Ausschnitt 72 x 72 wie bei der adaptiven Maske.
VOLL=$(mktemp)
trap 'rm -f "$VOLL"' EXIT
cat > "$VOLL" <<SVG
<svg xmlns="http://www.w3.org/2000/svg" viewBox="18 18 72 72">
  <rect x="18" y="18" width="72" height="72" rx="16" fill="$HINTERGRUND"/>
  <path fill="#FFFFFF" fill-rule="evenodd" d="$PFAD"/>
</svg>
SVG
for d in mdpi:48 hdpi:72 xhdpi:96 xxhdpi:144 xxxhdpi:192; do
  rsvg-convert -w "${d#*:}" -h "${d#*:}" -f png "$VOLL" -o "$RES/mipmap-${d%:*}/ic_launcher.png"
done
