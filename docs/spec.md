# exposer — the specification

Requirements are numbered so code can cite them: a comment saying "R-8" means R-8 below, and a test named after one asserts it. Section numbers are cited too (§11 Q6 fixes the URLs, §12 holds the acceptance criteria), so neither IDs nor sections are ever renumbered. The decisions behind these requirements are in AGENTS.md.

---

## 1. Purpose

A photography website derived **entirely and reproducibly** from an offline photo library. The library — originals plus sidecars — is the single source of truth; the site is a build artifact that can be deleted and regenerated at any time without losing anything.

## 2. Context

The library is what a photographer already has: originals developed in darktable, which writes XMP sidecars with hierarchical tags, star ratings and colour labels and never touches the original. The build runs on the machine holding the library; its output is static files for any static host.

## 3. Glossary

- **Library** — the directory tree of originals and sidecars.
- **Original** — a RAW or camera JPEG, never modified.
- **Sidecar** — the `<file>.xmp` beside an original.
- **Derivative** — a web-sized JPEG or AVIF rendered by the build.
- **Index** — the JSON extract of all metadata, written by stage 1.
- **Album** — a curated, named set of photos.
- **Listing** — a page of photos: an album, a tag, a location, a year or a month.

---

## 4. Source of truth: the library

**R-1 (MUST)** The build reads the library from a local path, read-only. No tool in the pipeline writes to an original or a sidecar.

**R-2 (MUST)** Everything the site shows about one photograph comes from metadata in the original or its sidecar. There is no hand-maintained file per photo.

**R-3 (SHOULD)** An album's title, intro and cover may live in `<library>/_data/albums/<slug>.yaml`, where `<slug>` is the slug of the album tag. *Because* albums are a tag namespace (§11 Q3), their photographs can sit in any directory, so there is no album directory to hold the file.

**R-4 (MUST)** Metadata contract — the fields consumed:

| Purpose | Field(s) |
|---|---|
| Capture time | `DateTimeOriginal`, fallback `CreateDate` |
| Title | `XMP-dc:Title` |
| Caption | `XMP-dc:Description` |
| Tags | `XMP-dc:Subject`, hierarchy from `XMP-lr:HierarchicalSubject` |
| Publication gate | `XMP:Rating`, `XMP:Label` |
| Location | `XMP-lr:HierarchicalSubject` under the `Location` namespace (R-10); GPS fields are never consumed |
| Technical | camera, lens, focal length, aperture, shutter, ISO |
| Rights and licence | `XMP-dc:Creator`, `XMP-dc:Rights` |

**R-5 (MUST)** A photo is published only if it passes a configurable gate (default: rating ≥ 4). Curation happens in darktable, not in the repository.

**R-6 (MUST)** Hierarchical tags (`Places|Harbour|Dock`) stay a hierarchy rather than being flattened to their leaves.

**R-7 (SHOULD)** Workflow tags never reach the site: neither un-namespaced tags (`todo`, `export`) nor editor namespaces such as `darktable|`. A namespaced tag is not automatically site content.

**R-8 (MUST)** The build fails loudly on a published photo that breaks the contract: no capture date, a sidecar that cannot be read, or no way to reach it — no display tag, no album and no location, which §12 criterion 4 needs for a page besides the timeline. Silent skipping is not acceptable.

Not violations: a missing title or caption (D-9 decides the `alt` text), and a photo with no sidecar at all, which has no rating and so cannot pass the gate. The build reports how many of those it saw.

**R-9 (MUST)** Directory structure carries no meaning; everything the build needs is metadata. The one exception is `<library>/_data/`, which describes the site rather than a photograph: `albums/` (R-3), `locations/` (R-10), `pages/` (R-13), `featured_photos.yaml` (R-16), `gear/` (R-17), `exposer.yaml` (R-18) and `tags/` (R-19). It holds no originals and is read-only like the rest (R-1).

**R-10 (MUST)** Locations are a tag namespace, like albums: `Location|Germany|Ulm`, never coordinates on the photo. Each node may be described in `<library>/_data/locations/<slug>.yaml`, the slug taken from the path below the namespace (`germany-ulm.yaml`), with a title, a description and one `lat`/`lon` pair for the place as a whole — the only coordinates the site ever uses.

**R-11 (MUST)** Coordinates from originals and sidecars are never read into the index and never written to a derivative. A private place is one described without `lat`/`lon`, so there is no per-photo location to leak.

**R-12 (MUST)** The same original imported twice is one photograph. Byte-identical originals share an id (§11 Q7), so the build keeps the lexicographically first path, whatever the scan order (F-3). If their sidecars disagree about curation — rating, tags, title, caption, album, location — the build fails naming both paths rather than picking a winner.

**R-13 (SHOULD)** Standalone pages — an imprint, an about page — may live at `<library>/_data/pages/*.md`: Markdown with optional YAML front matter for the title. They are site content, and live in the library because the library is what the site is derived from.

**R-14 (MUST)** A photograph may belong to any number of albums. It then:

- appears in each album's grid, with a scoped page (F-11a) beneath each;
- names every album on its own page (F-11);
- carries a different plate number in each (D-6), since the number is a position, not an identity;
- stays visible through a listed album when it is also in an unlisted one (F-18): unlisting withdraws an album, never a photograph.

Two albums may share a cover.

**R-15 (SHOULD)** Camera and lens, where the original records them, become tags under `Gear`, with the kind as its own level: `Gear|Camera|NIKON Z 6_2`, `Gear|Lens|NIKKOR Z 24-70mm f/4 S`. The kind orders listings (bodies before glass); the chips show only the equipment.

Gear tags are **derived, not curated**, so they do not count as a way to reach a photo under R-8: nearly every photo has a camera, which would make that check vacuous.

**R-16 (SHOULD)** Featured photographs may be listed in `<library>/_data/featured_photos.yaml`: library-relative paths, in display order. A path naming nothing, or a photo the gate holds back (R-5), fails the build; a path listed twice counts once. With no file, the index shows a sample instead (F-22).

**R-17 (SHOULD)** Gear may be described in `<library>/_data/gear/<name>.md`, where `<name>` is the equipment's slugified name — `dmc-g6.md` for `Gear|Camera|DMC-G6`. Markdown, optional per item. A file naming no gear in the library fails the build: a description nobody can see is worse than none.

**R-18 (SHOULD)** The generator's configuration lives in the library at `<library>/_data/exposer.yaml`: base URL, title, subtitle, footer line and imprint, the publication gate (R-5) and the derivative ladder (F-5). A key it does not know fails the build, naming the key — and a renamed setting its new name — rather than being ignored. Without one the build uses defaults; `--config` overrides it, so one library can be published twice with different settings. `exposer init <library>` writes a commented file holding exactly the defaults, and never replaces an existing one — the only file exposer ever writes into a library.

**R-19 (SHOULD)** Tags may be described in `<library>/_data/tags/<slug>.md`, `<slug>` being the tag's own URL slug — `category-architecture.md` for `Category|Architecture`. Markdown, optional per tag, shown on the tag's listing (F-8) and on the tag index (F-26). Gear uses R-17 instead. As with R-17, a file naming no tag fails the build, whether or not anything renders it.

---

## 5. Functional requirements

### 5.1 Generation

**F-1 (MUST)** One command produces the complete site from the library: index extraction → derivative rendering → site generation → assembly (B-8).

**F-2 (MUST)** The build is incremental: an unchanged photo is not re-rendered. The cache is keyed by the source hash, the rendering parameters and the versions of the tools that render.

**F-3 (MUST)** The build is deterministic: the same library and configuration yield byte-identical output, timestamps aside.

**F-4 (SHOULD)** RAW originals are developed with `darktable-cli original.raw original.raw.xmp out.jpg`, reproducing the darktable edit headlessly. Pre-exported JPEGs remain a supported alternative. Pixel dimensions come from `Composite:ImageSize`, which RAW and JPEG both provide, and are mandatory: D-7 needs them to reserve layout.

**F-5 (MUST)** Each photo gets derivatives at several widths for `srcset` (default 200 / 400 / 800 / 1600 / 2400 px long edge, configurable under R-18), and square (1:1) derivatives centre-cropped from it. Squares serve every place that needs a uniform tile and never appear in a photo's own `srcset`. A square is bounded by the short edge; where a photo is too small for a size, the caller falls back to the uncropped ladder. Nothing is upscaled.

A **sample grid** is up to eleven squares plus a twelfth cell that links to the full listing and names its count. The index sections of F-7, F-24 and F-26 use it, so the sample and the way past it form one block.

### 5.2 Views

**F-6 (MUST)** Album view: every photo of an album in a justified grid (D-4), ordered by capture time. There is no per-album override — one ordering everywhere keeps album, tag, location and timeline listings consistent.

**F-7 (MUST)** Album index at `/photos/albums/`, in the site menu (F-21): one section per listed album, a sample grid (F-5) of its first eleven photos in album order on one side of the rail, and its title, year span and intro on the other. The twelfth cell always links into the album, whatever the count. The year span covers the album's published photos — `2024` or `2024–2026`; the album's own `date` still orders the index and the feed (F-19). The front page lists albums plainly, as it lists years, tags and places.

**F-8 (MUST)** Tag views: one page per published tag at `/photos/tags/<slug>/`. `Album|` and `Location|` tags are not tags here; they have F-7 and F-8a.

**F-8a (MUST)** Location views: one page per location node at `/photos/locations/<slug>/`, listing every photo taken there or anywhere below it, with the title and description from its R-10 file.

**F-9 (SHOULD)** Multi-tag filtering (intersection of two or more tags) on the client, from a generated JSON index. Per-tag pages remain the canonical, crawlable entry points.

**F-10 (MUST)** Timeline at `/photos/<year>/` and `/photos/<year>/<month>/`, and whole at `/photos/timeline/` in the site menu: every published photo, newest first, under a heading per year that links to that year's listing. Its photos link into the year listings, so it adds no scoped pages of its own.

**F-11 (MUST)** One album-independent page per published photo at `/photos/p/<id>/`: the large derivative, title, caption, linked tags, capture date linked into the timeline, technical data, and every album the photo belongs to (R-14).

**F-11a (MUST)** The same photo seen inside a listing is its own page beneath that listing — `/photos/albums/<album>/<id>/`, and likewise under a tag, a location, a year and a month — with the listing's context and previous/next within it. Gear tags are the exception (R-15): their grids link to F-11. A scoped page declares `/photos/p/<id>/` canonical and stays out of the sitemap. *Because* a page with its own address survives being copied and works without JavaScript, which is what an overlay could not do (F-12).

**F-12 — withdrawn.** A lightbox overlay over every grid. Scoped pages (F-11a) replaced it; D-11 provides the large view.

**F-13 (MAY)** Map view from a self-hosted or privacy-neutral tile source, plotting locations, not photos: one marker per location with `lat`/`lon` in its R-10 file, linking to its view.

**F-14 (MAY)** Series or essay pages mixing text and photos.

**F-22 (SHOULD)** The index shows the featured photos (R-16) as small squares beside its lists, up to 24, in the library's order, each linking to F-11. Without a featured list it shows a sample ordered by photo id — a content hash, so the choice looks random yet is the same on every build (F-3).

**F-23 (SHOULD)** "All" page at `/photos/all/`, in the menu: every published photo as a square, newest first, ungrouped, linking to F-11 — the counterpart of F-10, showing how large the library is rather than when.

**F-24 (SHOULD)** Gear page at `/photos/gear/`, in the menu, under the headings Cameras and Lenses (R-15). Cameras are ordered by name; lenses by the focal length in their name, a zoom at its wide end, a lens without one last. The index's gear chips use the same order. Each item gets a section: a sample grid (F-5) of its most recent photos on one side of the rail, and its name, its R-17 description and its tag chip on the other.

**F-25 (SHOULD)** On a scoped page (F-11a), previous and next are square thumbnails with their direction written beneath, staying inside the listing. The link out to F-11 is styled like the fullscreen control (D-11): both leave this view of the photo.

**F-26 (SHOULD)** Tags page at `/photos/tags/`, in the menu: one section per curated tag in slug order, a sample grid (F-5) of its most recent photos on one side of the rail, and its R-19 description and its chip on the other. Gear tags are left out (F-24). The page grants no reachability under §12 criterion 4, which counts a single tag's listing.

### 5.3 Publication and privacy

**F-15 (MUST)** Originals are never published; only derivatives are.

**F-16 (MUST)** Derivative metadata is controlled: creator and copyright kept, GPS and camera serial always stripped, with no override (R-11).

**F-17 (SHOULD)** Location granularity is a curation decision: how deep a place is tagged (R-10), and whether it is given coordinates at all (R-11).

**F-18 (SHOULD)** An album can be unlisted (`unlisted: true` in its R-3 file): reachable by URL, left out of indexes, feeds and the sitemap. Unlisting withdraws the album, never its photos (R-14), which still appear on their tag, location and timeline pages; a photo that must not be found has to fail the gate (R-5).

**F-19 (SHOULD)** RSS/Atom feed of new albums; `sitemap.xml` for the whole photo section.

**F-20 (MUST)** Open Graph tags on every album and photo page.

**F-21 (SHOULD)** Each R-13 page is rendered at `/photos/<slug>/`, `<slug>` being the file's stem, in the sitemap but not the feed. `menu: true` in its front matter lists it in the site menu and `weight` orders it; pages are unlisted by default, since some, like the imprint (D-10), are reached from elsewhere. A slug colliding with a year or with `albums`, `tags`, `locations` or `p` fails the build rather than shadowing it.

---

## 6. Design requirements

Target: crisp, minimal, light, editorial — the photograph is the only thing with colour on the page.

**D-1 (MUST)** Light theme only for v1. Ground `#FAFAFA`, text `#111111`, hairlines `#E5E5E5`. No accent colour.

**D-2 (MUST)** No shadows, rounded corners, gradients or hover zoom. Transitions only on opacity, ≤ 200 ms, and none under `prefers-reduced-motion`.

**D-3 (MUST)** One grotesque for all UI text, Inter Tight: self-hosted, latin-subset, `font-display: swap`, preloaded so the swap does not shift the layout (D-7). Captions ~11px, letterspaced, uppercase. Technical data in IBM Plex Mono.

**D-4 (MUST)** A listing's grid is justified rows with gutters of 4–8px, each photo shown in its own shape. The sample grids of F-5 are the one place photos are cropped square.

Two deviations, both deliberate. **Row heights vary** between rows rather than being fixed: the rows are CSS flexbox, each photo growing with its aspect ratio, which needs no JavaScript and never reflows (D-7, N-3); a script could fix the height but would shift the page after first paint. And **a frame taller than 2:3 is laid out at 2:3 and centre-cropped**, because a narrower one adds almost nothing to its row while taking a slot, stretching every photo beside it. Every other shape, panoramas included, is shown whole, and F-11 always shows the whole frame.

**D-5 (MUST)** Generous margins and a visible baseline; content aligned to a simple grid (12 columns, or a 3/9 asymmetric split).

**D-6 (SHOULD)** Plate numbers and hairline rules are the only ornament.

**D-7 (MUST)** Layout is stable while images load: dimensions known in advance, no reflow, and a flat tone — the photo's average colour — until the image decodes.

**D-8 (MUST)** Responsive down to 360px; on small screens the grid becomes one column at full bleed.

**D-9 (MUST)** WCAG AA contrast, visible focus, full keyboard navigation of every grid and of the fullscreen view, and non-empty `alt` on every photo: the caption, else the title, else the photo id. An id in `alt` is a placeholder, not a description — publishing an untitled photo poorly described beats not publishing it, but each one is curation debt.

**D-10 (SHOULD)** The site stands alone, with configurable title, subtitle and imprint (R-18) and short links to other pages. The imprint is an R-13 page; the configuration names the slug the footer links to.

**D-11 (SHOULD)** On a photo page (F-11, F-11a), clicking the photo shows it in the browser's fullscreen mode, centred and as large as the screen allows without changing its ratio. Space the photo does not fill is the site's own ground, `#FAFAFA` (D-1), not black. Entering and leaving fullscreen works from the keyboard, and the image keeps its `alt` (D-9).

---

## 7. Non-functional requirements

**N-1 (MUST)** No server-side runtime: plain files, served by any static host.

**N-2 (MUST)** No third-party request at page load: no CDN fonts, analytics, tracking pixels or externally hosted scripts.

**N-3 (MUST)** An album view's initial weight is ≤ 1.5 MB including above-the-fold images; JavaScript ≤ 100 KB gzipped.

**N-4 (SHOULD)** Lighthouse performance ≥ 90 on an album page of 60 photos.

**N-5 (MUST)** A cold build of 1,000 published photos takes ≤ 30 min on a decent desktop; a warm build ≤ 3 min.

**N-6 (MUST)** The library scales to 10,000 published photos without a change of architecture.

**N-7 (SHOULD)** Tooling runs in one pinned container image, so every machine renders identically. Not built; planned in AGENTS.md. Until then the binary pins Hugo (B-8), the derivative cache keys on the versions of the tools that render, so a changed tool re-renders rather than mixing output, and CI runs in `debian:trixie`.

---

## 8. Build and deployment

**B-1 (MUST)** Three separate locations: the generator (this repository), the library (a local directory, R-1) and the output (a local directory a deployment may publish).

**B-2 (MUST)** Merged into R-1: the library is a local path, read-only.

**B-3 (MUST)** Merged into B-5.

**B-4 (MUST)** The site builds correctly under a configurable base URL, domain or path (R-18).

**B-5 (MUST)** The output directory holds the artifact, `site/`, and beside it the index, the manifests and the caches, keyed by content hash so the next build is incremental (F-2). Only `site/` is ever deployed: static files with no build state in them.

**B-6 (SHOULD)** Deployment is idempotent and removes files the build no longer produces. It belongs to a site, not to the generator, which builds into a local directory and stops.

**B-7 (SHOULD)** `exposer serve` previews `<target>/site` on 127.0.0.1:8888 as a static host would: a directory resolves to its `index.html`, a path without its trailing slash redirects to one with it, and AVIF and woff2 carry their real media types — served as `application/octet-stream`, AVIF would silently fall back to JPEG. `--target` lets branches and libraries preview side by side.

**B-8 (MUST)** The generator ships as one binary: `exposer build <library>` runs everything, and the theme and index schema are compiled in, so it runs from any directory and never renders with another version's templates (`--theme`, `--schema` override). Hugo is pinned with the SHA-256 of each release archive, downloaded into the user's cache on first use and refused on a mismatch; where no archive exists (macOS ships an installer only), a `hugo` on `PATH` must report the pinned version; `--hugo` names one directly. The pin serves F-3. Each release tag also publishes static binaries for Linux and macOS on amd64 and arm64, built with `-trimpath` so the same tag builds the same bytes, each stamped with exactly its tag, listed in `SHA256SUMS` and attested by the workflow that built it. `exposer --version` reports the release and the pinned Hugo together, since both decide a build's bytes. exiftool, ImageMagick and darktable (for RAW) are external requirements.

---

## 9. Technology decision

Hugo, by prototype, over Astro. AGENTS.md records why.

## 10. Out of scope (v1)

Sales and payment; comments and likes; client galleries, passwords, per-user access; uploading or editing through the web; face recognition or automatic tagging; multilingual content; video.

---

## 11. Settled questions

Asked before anything was built, and answered; numbered because code cites them.

1. **Where does the build get the library?** Locally: the library never leaves its host. CI builds only `testdata/`.
2. **RAW or exported JPEGs?** Both (F-4).
3. **Albums by directory or by tag?** By tag namespace, `Album|<name>` (R-3).
4. **A separate publication date?** No; the timeline uses capture date.
5. **AVIF as well as JPEG?** Yes, both.
6. **URL layout?** Everything generated lives under `/photos/`: `/photos/albums/<slug>/`, `/photos/tags/<slug>/`, `/photos/locations/<slug>/`, `/photos/<year>/<month>/`, `/photos/p/<id>/`, and a scoped page beneath each listing (F-11a). The site root `/` carries the index itself; `/photos/` repeats it with `/` as its canonical URL, out of the sitemap.
7. **Photo ids?** A content hash of the original, truncated for URLs, cached by path, size and mtime. It survives renaming and re-editing, since darktable writes only the sidecar.

---

## 12. Acceptance criteria for v1

1. Deleting the generated site and rebuilding reproduces it exactly (F-3).
2. Adding a tag in darktable and rebuilding puts the photo on that tag's page, with no other step.
3. Raising a photo's rating to 4 publishes it; lowering it removes it from every listing and the sitemap.
4. Every published photo is linked from the timeline and from at least one other listing: an album, a tag or a location. `exposer assemble` checks the built site and fails otherwise. Only listing pages count — not scoped pages, which would let one photo vouch for the next, and not the All page (F-23) or gear tags (R-15), which hold everything and would make the check vacuous.
5. No request leaves the browser for a third-party domain (N-2).
6. An album page of 60 photos scores ≥ 90 in Lighthouse performance (N-4) and passes axe with no serious violations.
