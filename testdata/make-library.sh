#!/usr/bin/env bash
# Build the test libraries under testdata/.
#
# The images this writes are committed, so this script is not part of the build
# and CI never runs it: a clone tests against bytes, not against whatever
# ImageMagick happens to be installed. Run it by hand when the fixtures need to
# change, and commit what it produces.
#
# Every photograph here exists to cover a specific requirement. testdata/README.md
# says which, photograph by photograph, and that mapping is the point of the
# fixture -- a library of pretty pictures would test nothing in particular.
set -euo pipefail

cd "$(dirname "$0")"
root=library
rm -rf "$root" broken
mkdir -p "$root"

# A photograph, and the sidecar darktable would have left beside it.
#   make <path> <width>x<height> <colour> <camera> <lens> <taken>
make() {
  local path=$1 size=$2 colour=$3 camera=$4 lens=$5 taken=$6
  mkdir -p "$(dirname "$path")"
  magick -size "$size" "gradient:${colour}" \
    -fill '#ffffff' -pointsize 40 -annotate +24+64 "$(basename "$path" .jpg)" \
    -quality 62 "$path"
  local args=(-overwrite_original -q)
  [ "$camera" != "-" ] && args+=("-Make=Testmaker" "-Model=$camera")
  [ "$lens" != "-" ] && args+=("-LensModel=$lens")
  [ "$taken" != "-" ] && args+=("-DateTimeOriginal=$taken" "-CreateDate=$taken")
  exiftool "${args[@]}" -Artist='A. Photographer' "$path"
}

# The sidecar. Tags arrive as a hierarchy; the flat dc:subject carries the
# leaves, the way darktable writes them.
#   sidecar <path> <rating> <title> <description> <tag>...
sidecar() {
  local path=$1 rating=$2 title=$3 description=$4
  shift 4
  local deep="" flat=""
  for tag in "$@"; do
    deep+="<rdf:li>${tag}</rdf:li>"
    flat+="<rdf:li>${tag##*|}</rdf:li>"
  done
  {
    printf '<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>\n'
    printf '<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="exposer testdata">\n'
    printf ' <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">\n'
    printf '  <rdf:Description rdf:about=""\n'
    printf '    xmlns:xmp="http://ns.adobe.com/xap/1.0/"\n'
    printf '    xmlns:dc="http://purl.org/dc/elements/1.1/"\n'
    printf '    xmlns:lr="http://ns.adobe.com/lightroom/1.0/"\n'
    printf '   xmp:Rating="%s">\n' "$rating"
    printf '   <dc:creator><rdf:Seq><rdf:li>A. Photographer</rdf:li></rdf:Seq></dc:creator>\n'
    [ -n "$title" ] && printf '   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:title>\n' "$title"
    [ -n "$description" ] && printf '   <dc:description><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:description>\n' "$description"
    [ -n "$deep" ] && printf '   <dc:subject><rdf:Bag>%s</rdf:Bag></dc:subject>\n' "$flat"
    [ -n "$deep" ] && printf '   <lr:hierarchicalSubject><rdf:Bag>%s</rdf:Bag></lr:hierarchicalSubject>\n' "$deep"
    printf '  </rdf:Description>\n </rdf:RDF>\n</x:xmpmeta>\n'
  } > "${path}.xmp"
}

CAM_A="TESTCAM A1"; CAM_B="TESTCAM B2"
L_ZOOM="TEST 12-40mm F2.8"; L_SLASH="TEST 7-14/F4.0"
L_FAST="TEST 45mm F1.8";    L_WIDE="TEST 35mm F2.0"

# 01 the complete photograph: title, caption, album, place, curated tag, gear.
make $root/2024/03/harbour-dawn.jpg 900x600 '#123b5c-#c9d6df' "$CAM_A" "$L_ZOOM" '2024:03:04 07:12:00'
sidecar $root/2024/03/harbour-dawn.jpg 5 'Harbour at dawn' 'The first light over the container cranes.' \
  'Album|Harbour' 'Location|Norway|Bergen' 'Places|Harbour'

# 02 no title and no caption: D-9's alt text falls back to the id.
make $root/2024/03/harbour-boats.jpg 900x600 '#2a4d3a-#e8e2d0' "$CAM_A" "$L_SLASH" '2024:03:04 09:40:00'
sidecar $root/2024/03/harbour-boats.jpg 4 '' '' \
  'Album|Harbour' 'Location|Norway|Bergen' 'Places|Harbour'

# 03 a panorama, for the wide end of D-4's justified rows.
make $root/2024/03/harbour-crane.jpg 900x300 '#4a2f1b-#d8c7a8' "$CAM_A" "$L_FAST" '2024:03:04 11:05:00'
sidecar $root/2024/03/harbour-crane.jpg 4 'Crane' '' \
  'Album|Harbour' 'Subject|Industry'

# 04 a 9:16 portrait, narrower than 2:3: the only shape grids crop (D-4).
make $root/2024/07/fjord-wall.jpg 506x900 '#1d2b33-#b4c6cf' "$CAM_B" "$L_WIDE" '2024:07:18 16:20:00'
sidecar $root/2024/07/fjord-wall.jpg 4 'Rock wall' '' \
  'Album|Private Set' 'Location|Norway'

# 05 second member of the unlisted album (F-18).
make $root/2024/07/fjord-water.jpg 900x600 '#14343f-#cfe0e4' "$CAM_B" "$L_WIDE" '2024:07:18 17:02:00'
sidecar $root/2024/07/fjord-water.jpg 5 'Still water' 'Nothing moved for an hour.' \
  'Album|Private Set' 'Location|Norway'

# 06 tags that must never reach the site (R-7), beside one that must: a flat tag
# and a darktable tag are both filtered, so the namespaced one is also what
# keeps this photograph reachable (R-8).
make $root/2023/11/street-lamp.jpg 900x600 '#2b2118-#e0d4c3' "$CAM_B" "$L_WIDE" '2023:11:02 21:15:00'
sidecar $root/2023/11/street-lamp.jpg 4 'Lamp' '' \
  'Subject|Street' 'darktable|exported' 'todo'

# 07 the same bytes at a second path, with the same metadata: R-12 collapses it.
mkdir -p $root/inbox
cp $root/2023/11/street-lamp.jpg $root/inbox/copy-of-lamp.jpg
sidecar $root/inbox/copy-of-lamp.jpg 4 'Lamp' '' \
  'Subject|Street' 'darktable|exported' 'todo'

# 08 below the gate, and 09 never rated at all (R-5).
make $root/2022/05/rejected.jpg 900x600 '#3a3a3a-#bdbdbd' "$CAM_B" "$L_WIDE" '2022:05:09 12:00:00'
sidecar $root/2022/05/rejected.jpg 3 'Not good enough' '' 'Subject|Street'
make $root/2022/05/untouched.jpg 900x600 '#33383a-#c4c9cc' "$CAM_B" "$L_WIDE" '2022:05:09 12:30:00'

# 10 a square frame, and a lens shared with 01 so gear pages hold more than one.
make $root/2024/12/square-window.jpg 700x700 '#243447-#dfe6ea' "$CAM_B" "$L_ZOOM" '2024:12:01 13:45:00'
sidecar $root/2024/12/square-window.jpg 4 'Window' '' \
  'Shapes|Square' 'Location|Norway|Bergen'

# 11 three levels of hierarchy (R-6), rendered with the » separator.
make $root/2025/01/dock-detail.jpg 900x600 '#1b2a36-#c8d2d8' "$CAM_A" "$L_ZOOM" '2025:01:22 10:10:00'
sidecar $root/2025/01/dock-detail.jpg 4 'Bollard' '' \
  'Places|Harbour|Dock' 'Location|Norway|Bergen'

# 12 no camera and no lens in EXIF, so it earns no gear tags (R-15); and its
# album is its only curated path, so emptying that listing strands it and
# nothing else (§12 criterion 4).
make $root/2025/01/scanned-print.jpg 900x600 '#3b3128-#ded3c4' - - '2025:01:30 15:00:00'
sidecar $root/2025/01/scanned-print.jpg 4 'From a print' '' \
  'Album|Harbour'

mkdir -p $root/_data/albums $root/_data/locations $root/_data/pages $root/_data/gear

cat > $root/_data/albums/harbour.yaml <<'YAML'
# Keyed by the album tag's slug; the file says which tag it backs (R-3).
tag: "Album|Harbour"
title: Harbour
intro: |
  A morning spent walking the working edge of the port, before anyone
  else was awake.
cover: harbour-dawn.jpg
YAML

cat > $root/_data/albums/private-set.yaml <<'YAML'
tag: "Album|Private Set"
title: Private Set
intro: Reachable by its address, absent from every listing.
unlisted: true
YAML

cat > $root/_data/locations/norway.yaml <<'YAML'
tag: "Location|Norway"
title: Norway
description: A place described without coordinates stays off the map.
YAML

cat > $root/_data/locations/norway-bergen.yaml <<'YAML'
tag: "Location|Norway|Bergen"
title: Bergen
lat: 60.3913
lon: 5.3221
YAML

cat > $root/_data/pages/about.md <<'MD'
---
title: About
menu: true
---

A standalone page from the library, rendered beside the photographs.
MD

cat > $root/_data/gear/testcam-a1.md <<'MD'
The body most of these frames came from. This file exists to prove a gear
description renders when the library offers one.
MD

# R-19: prose about one tag, shown on the tag index and on the tag's listing.
mkdir -p $root/_data/tags
cat > $root/_data/tags/subject-street.md <<'MD'
Photographs made *in public*, with nobody asked to stand anywhere.

This file exists to prove a tag description renders Markdown when the library offers it.
MD

# R-18: the library carries the generator's configuration too.
cat > $root/_data/exposer.yaml <<'YAML'
base_url: "/"
title: "Test Library"
subtitle: "The library the tests build"
imprint_page: ""
footer_line: "© 2026 The Test Library"
min_rating: 4
derivatives:
  widths: [200, 400, 800, 1200, 1600, 2400]
  square_widths: [200, 400]
  formats: ["jpeg", "avif"]
  jpeg_quality: 82
  avif_quality: 50
YAML

cat > $root/_data/featured_photos.yaml <<'YAML'
# R-16: the photographer's own selection, in their own order.
- 2024/03/harbour-dawn.jpg
- 2024/07/fjord-water.jpg
- 2025/01/dock-detail.jpg
YAML

# The broken libraries. Each one trips exactly one hard failure, because a build
# that is supposed to fail loudly needs a test that proves it does.
one_good() { # a valid published photograph, so the library fails for one reason only
  local dir=$1
  make "$dir/2024/03/fine.jpg" 900x600 '#123b5c-#c9d6df' "$CAM_A" "$L_ZOOM" '2024:03:04 07:12:00'
  sidecar "$dir/2024/03/fine.jpg" 4 'Fine' '' 'Album|Harbour' 'Places|Harbour'
  mkdir -p "$dir/_data/albums"
  printf 'tag: "Album|Harbour"\ntitle: Harbour\n' > "$dir/_data/albums/harbour.yaml"
}

# R-8: published, but nothing says when it was taken.
one_good broken/no-capture-date
make broken/no-capture-date/undated.jpg 900x600 '#402020-#d0c0c0' "$CAM_A" "$L_ZOOM" -
sidecar broken/no-capture-date/undated.jpg 4 'Undated' '' 'Places|Harbour'

# R-8: published, but reachable from nothing except the timeline and its gear.
one_good broken/no-path
make broken/no-path/orphan.jpg 900x600 '#204020-#c0d0c0' "$CAM_A" "$L_ZOOM" '2024:04:01 09:00:00'
sidecar broken/no-path/orphan.jpg 4 'Orphan' '' 'darktable|exported' 'todo'

# R-12: the same bytes twice, curated differently — which rating is true?
one_good broken/duplicate-conflict
make broken/duplicate-conflict/first.jpg 900x600 '#202040-#c0c0d0' "$CAM_A" "$L_ZOOM" '2024:05:01 09:00:00'
cp broken/duplicate-conflict/first.jpg broken/duplicate-conflict/second.jpg
sidecar broken/duplicate-conflict/first.jpg 4 'First' '' 'Places|Harbour'
sidecar broken/duplicate-conflict/second.jpg 5 'Second' '' 'Places|Harbour'

# R-16: a featured list naming a photograph that does not exist.
one_good broken/unknown-featured
mkdir -p broken/unknown-featured/_data
printf -- '- 2024/03/not-here.jpg\n' > broken/unknown-featured/_data/featured_photos.yaml

# R-17: a gear description naming no camera and no lens in the library.
one_good broken/unknown-gear
mkdir -p broken/unknown-gear/_data/gear
printf 'Nothing in the library is called this.\n' > broken/unknown-gear/_data/gear/nonexistent.md

# R-19: a tag description naming no tag in the library.
one_good broken/unknown-tag
mkdir -p broken/unknown-tag/_data/tags
printf 'No tag has this slug.\n' > broken/unknown-tag/_data/tags/nonexistent.md

echo "library: $(find $root -name '*.jpg' | wc -l) photographs, $(du -sh $root | cut -f1)"
echo "broken:  $(ls broken | tr '\n' ' ')"
