# exposer

A static photography site generator: an offline library of originals and XMP
sidecars in, static files out. The full specification is `docs/spec.md`, and its
requirements are numbered (R-, F-, D-, N-, B-) so that code can cite them. Read
it before planning work; cite the IDs in commit messages.

## Invariants

Violating one of these is a bug, not a tradeoff.

- **The library is read-only.** Nothing here writes to an original or a sidecar.
- **Per-photo metadata comes only from sidecars and EXIF.** A library may also
  carry `_data/albums/` (R-3), `_data/locations/` (R-10), `_data/pages/` (R-13),
  `_data/featured_photos.yaml` (R-16), `_data/gear/` (R-17), `_data/tags/`
  (R-19) and the generator's own configuration at `_data/exposer.yaml` (R-18).
  Those describe sets of photographs and the site; never one photograph.
- **Output is static files.** No server runtime, no third-party request at page
  load (N-2).
- **Builds are incremental and deterministic.** The same library produces the
  same bytes (F-3), or a deploy cannot tell a change from a rebuild.

## Layout

```
cmd/exposer/   the binary: every stage, one package
embed.go       compiles site-gen/ and schema/ into it (B-8)
schema/        the JSON Schema stage 1's output must satisfy
site-gen/      the Hugo theme and templates
testdata/      the fixture libraries; testdata/README.md maps each photograph
               to the requirement it covers
docs/spec.md   the numbered specification
target/        build output (site/) and caches (cache/); never committed
```

## Commands

`exposer init <library>` writes a default configuration into `_data/`.
Every command takes `-v` (why photographs were left out, which were
rendered, what each stage took) and `-q` (errors only); output goes through `report`,
`notice` and `detail` in `output.go`, never straight to `fmt`.
`exposer build <library>` is the whole pipeline, into `target/site`
(`--target` moves it). `exposer serve` previews it. Each stage is also a
subcommand, for running one at a time.

Working on exposer itself, the Makefile wraps those:

| Command | What it does |
|---|---|
| `make build` | builds the fixture library (`LIBRARY` and `TARGET` override) |
| `make serve` | build, then serve at http://127.0.0.1:8888 (`PORT` overrides) |
| `make test` | the suite; unit tests need nothing installed, the rest skip |
| `make lint` | gofmt and go vet, what CI checks first |
| `make check` | build, then validate `target/index.json` against the schema |

## Decisions

These are settled. Build on them; do not reopen them file by file.

**Hugo, not a JavaScript generator.** Chosen by prototype: two throwaway
implementations of the same album view, identical markup, so the comparison was
about tooling. Design fidelity tied — both emitted the same `srcset` and
intrinsic dimensions. Build performance favoured Hugo (0.49 s against 2.4 s for a
5,000-photo page) but neither was a bottleneck. The decision rested on
maintenance: **one pinned binary against 246 npm packages**, for a site that
should still build unattended in five years. Two arguments that looked decisive
beforehand turned out moot — neither generator can develop a RAW (F-4), so stage
2 renders every derivative and the generator only ever emits HTML.

**Go only.** Stage 1 was once implemented twice, Python and Go, held identical
by a comparison target. That experiment paid for itself once, catching a
pointer-aliasing bug neither implementation would have found alone, and was then
retired: carrying two implementations through four more stages means every
change lands twice, forever. A stage that needs a second language needs a
different design. What partly replaces the cross-check is asserted instead — the
schema, F-3's byte-identical rebuild, F-2's pure derivative cache, and F-15's
assembly check.

**Derivatives never enter the Hugo tree.** Hugo publishes page-bundle resources
by default, which put originals into the output during the prototype — a direct
F-15 violation. Derivatives are hardlinked into the artifact during assembly
instead, and `exposer assemble` fails the build if any file in `target/site`
hashes to an original. The same stage enforces §12 criterion 4: every published
photograph must be linked from the timeline and from one other listing.

**The configuration lives in the library**, at `<library>/_data/exposer.yaml`
(R-18). It sat beside the generator once, which is the one place it certainly
does not belong: a generator knows nothing about any particular site. `_data/`
already holds album intros, place descriptions, the imprint page, the featured
list and the gear notes; the configuration describes the same site those
describe. A library is therefore the whole site — hand the directory to someone
and they can rebuild it. `--config` still overrides, so one library can be
published twice with different settings.

**The module is the repository.** `go.mod` is at the root, `module
github.com/berb/exposer`, with the command at `cmd/exposer/` and the schema at
`schema/`. A module's path is the directory holding its `go.mod`, and after the
first tag that path is a promise to everyone who imports it.

**One binary, with Hugo pinned.** `exposer build <library>` runs everything,
and the theme and schema are compiled in (B-8), so an installed binary works
from any directory and can never render with another version's templates. Hugo
is a separate program and cannot be embedded; the binary pins its version and
the SHA-256 of each release archive, downloads it into the user cache on first
use, and refuses anything that does not match. The pin serves F-3 — a
byte-identical rebuild holds only while the renderer stays put. The Makefile is
a development convenience and nothing a user needs.

**The tool knows nothing about any deployment.** It builds a library into a
local directory and stops. Publishing is a property of a site, not of a
generator.

## Planned

Agreed, not yet built. Build it this way or change this section first.

**A pinned container image, for N-7 — after v0.1.0.** The binary pins what it
can (B-8): the theme, the schema and Hugo. It cannot pin ImageMagick, libheif,
libaom, exiftool or darktable, and those decide the bytes of every derivative,
so F-3's byte-identical rebuild holds per machine rather than across machines.
The cache key includes those tool versions, so a different machine means a full
re-render and a full re-upload, never stale output. The image closes that gap,
and it also covers macOS and Windows, which the CLI handles poorly or not at all.

- **One Dockerfile is the toolchain's only definition.** `FROM debian:trixie`,
  the same packages CI installs today (exiftool, ImageMagick 7, and the
  `libheif-plugin-aomenc` and `libheif-plugin-dav1d` plugins that AVIF needs),
  the exposer binary, and Hugo already fetched so a run needs no network.
  `ENTRYPOINT ["exposer"]`.
- **CI builds the image and runs the suite inside it**, replacing the package
  list in `.github/workflows/ci.yml`, so a green run describes what users
  download.
- **Two tags per release:** `ghcr.io/berb/exposer:<version>` without darktable,
  for JPEG libraries, and `<version>-raw` with it, labelled with its darktable
  version. Multi-arch, amd64 and arm64.
- **Pin by digest, not by tag.** Debian stable changes package versions in
  point releases, so only a digest guarantees the same tools.
- **The documented command mounts the library read-only** and runs as the
  caller, which enforces R-1 at the kernel rather than by convention and keeps
  root out of `target/`:

  ```sh
  docker run --rm --user "$(id -u):$(id -g)" \
    -v ~/photos:/library:ro -v "$PWD/target":/target \
    ghcr.io/berb/exposer:<version> build /library --target /target
  ```

- **The README offers both paths side by side:** the CLI with your own tools and
  your own versions, or the image with everything pinned.

**Open, and to be answered before the `-raw` image exists:** the edits come from
the photographer's desktop darktable, which writes the XMP history stack. If the
image carries an older darktable than the one that wrote a sidecar, modules it
does not know are dropped and the RAW develops differently from what the
photographer saw. For RAW files a pinned darktable can be a mismatch rather than
a guarantee. Decide which darktable the image carries, and whether exposer warns
when a sidecar was written by a newer one (F-4).

## Settled questions

`docs/spec.md` §11 has the answers in full. In short:

- Albums are a tag namespace (`Album|<name>`), not a directory convention.
  Locations work the same way, and their coordinates belong to the place rather
  than the photograph — per-photo GPS is never read and never published (R-11).
- A photo's id is a content hash of the original, truncated for URLs, cached by
  path, size and mtime. It survives renames and re-edits; the filename is not an
  input.
- `album.yaml` lives at `<library>/_data/albums/<slug>.yaml`, keyed by the album
  tag's slug — not in a per-album directory.
- There is no separate publication date. The timeline uses capture date.
- Both AVIF and JPEG derivatives are generated.
- The URL layout is fixed: `/photos/albums/<slug>/`, `/photos/tags/<slug>/`,
  `/photos/<year>/<month>/`, `/photos/p/<id>/`, and everything generated lives
  under `/photos/`.

## Design

The constraints live in the CSS at the top of `site-gen/layouts/baseof.html`.
`site-gen/assets/fullscreen.js` is the only JavaScript on the site and must stay
progressive enhancement: it enhances D-11's fullscreen view on a photo page and
ships its trigger hidden until the browser proves the API exists. Every grid
entry is a plain link to a real page. An earlier lightbox was withdrawn once
every listing gained scoped pages, and nothing may reintroduce a grid overlay —
a scoped page has an address that survives copying, works without JavaScript,
and carries its neighbours.

Known gap: D-4's justified grid is CSS-only, so row heights vary rather than
being fixed. The deviation is recorded in D-4.

## Working agreement

- Plan before implementing.
- Cite requirement IDs in commit messages. A change that fits no requirement is
  a change to `docs/spec.md` first.
- A change a user would notice gets a line under `## [Unreleased]` in
  `CHANGELOG.md`, in the same commit, in plain words and without requirement
  IDs. Releasing renames that section to the version and date; its text
  becomes the release notes, and the release workflow refuses a tag it has no
  section for.
- Tests come with the change, not after it — the hard-fail paths especially,
  since those are the ones nobody exercises by accident.
- Ask rather than guess when a requirement is genuinely ambiguous.
- Never commit the build output, a library, or anything under `tmp/`.
