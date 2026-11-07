# aniclitr — Architecture

A developer and agent guide. Read it before changing code. It explains how
the pieces fit, the invariants that keep the app correct, and the traps that
already cost time once.

User-facing docs live in `README.md`. Planned work is in `ROADMAP.md`,
released changes in `CHANGELOG.md`. The layout rules (page anatomy, the
kit, işlemler) are in `docs/DESIGN.md`; read it before changing anything on
screen.

---

## 1. What it is

aniclitr is a keyboard-driven terminal anime player, aimed at Turkish
viewers. It is a Go program with these parts:

- **UI:** Bubble Tea for the event loop, plus its own cell renderer
  (`internal/tty`).
- **Sources:** live scrapers and APIs (AnimeciX primary, HiAnime English,
  TürkAnime archive as legacy).
- **Playback:** mpv, controlled over JSON IPC.
- **Downloads:** aria2c / yt-dlp, with a built-in fallback.
- **Accounts:** MyAnimeList sync (OAuth PKCE).
- **Metadata:** AniList for discovery (schedule, seasons, recommendations,
  trailers).

**Product rules:**

1. **Keyboard only.** No mouse-driven features. A little legacy mouse
   support on home exists, but do not build on it.
2. **Data must be live.** No stale indexes. AnimeciX is the primary source;
   the TürkAnime archive is opt-in "legacy".
3. **Design matters a lot.** Things should look and feel finished and
   consistent, and be readable rather than crowded. Motion over colour
   noise; always keep a reduced-motion path.
4. **While mpv plays, the terminal stays idle.** No animation, no ticking.
5. **All UI text is Turkish.** Use Turkish casing:
   `strings.ToUpperSpecial(unicode.TurkishCase, …)`, never plain
   `strings.ToUpper`, which turns "i" into "I".

---

## 2. Repository layout

```
cmd/aniclitr/main.go      entry: flags, TTY check, logging, graphics probe, renderer choice
internal/
  config/                 Config (JSON), Dir()/CacheDir(), WriteAtomic
  logx/                   rotating log at %APPDATA%\aniclitr\aniclitr.log, Panic() with stack
  netx/                   shared HTTP client (Chrome UA, cookie jar, retries)
  source/                 provider contract, registry, health probe, URLOpener
    animecix/             primary: JSON API + tau-video
    animetr/              Turkish: search JSON + per-episode player JSON (OK.ru, Drive, Vidmoly, Voe)
    anihub/               Turkish: REST API + token-signed HLS streams
    hianime/              English: HTML + embed decoder (HLS)
    animedepo/            legacy archive (GitLab index, mostly dead links)
  extract/                self-registering host resolvers: tau, sibnet, Google Drive, OK.ru, Vidmoly, Voe, HLS/MP4 prober, typed errors, yt-dlp fallback
  match/                  unified title folding, Sorensen-Dice fuzzy scoring, persistent mappings.json cache
  seasons/                AniList PREQUEL/SEQUEL chain resolver and absolute-to-seasonal episode mapper
  choose/                 rule-based host & quality selector (preferred host, permanent error skip)
  diag/                   persistent source & host reliability telemetry (diag.json, doctor --blame)
  aniskip/                OP/ED timestamps (AniSkip v2)
  player/                 mpv launcher + JSON IPC (named pipe on Windows)
  store/                  history.json (progress, notes, activity), library.json (shelf, MAL mirror)
  download/               download manager (queue, aria2c / yt-dlp / native)
  anilist/                AniList GraphQL (schedule, season, genres, recs, trailers, MAL-id mapping)
  mal/                    MyAnimeList API v2 client + OAuth PKCE login
  malsync/                two-way library <-> MAL sync loop
  subs/                   external subtitles (OpenSubtitles; provider interface)
  sound/                  tiny UI sounds synthesised in memory (winmm on Windows)
  tty/                    the cell renderer: Frame, Diff, Writer, test Emulator
  doctor/                 `aniclitr doctor`: live probe of every source
  pathx/                  installed copy (%LOCALAPPDATA%\Programs\aniclitr) + user PATH (HKCU\Environment) / ~/.local/bin link
  update/                 GitHub Releases: latest, SHA-256 checked download, self-swap (.old)
  ui/                     everything on screen (see §4)
```

Module name: `aniclitr`. Go 1.27+.

---

## 3. Data and control flow

```
            ┌────────────── Bubble Tea event loop (tea.WithoutRenderer) ──────────────┐
 keys ────▶ │ App.Update(msg) ─▶ top screen .update()  (keys/mouse: top only)          │
 async ───▶ │                 ─▶ every screen .update() (data msgs: all screens)       │
            │ App.paint()  ─▶ compose() lines ─▶ tty.Frame ─▶ tty.Writer (goroutine) ─▶│──▶ terminal
            └──────────────────────────────────────────────────────────────────────────┘
 network / disk / mpv run in tea.Cmd goroutines (ui.run / guard) and come back as messages
```

- `App` (`ui/app.go`) is the single Bubble Tea model. It owns:
  - the **screen stack** (`a.stack`) and its overlays (palette, popup,
    dropdown, help);
  - the caches (`details`, `episodes`, `variants`, `resolved`, `posters`);
  - the stores (`hist`, `lib`, `dl`, `mal`, `msync`).
- **Nothing blocks the loop.** Every network or disk call runs inside
  `run(timeout, func(ctx) tea.Msg)` or `guard(name, fn)`. Both recover
  panics, log them, and turn them into a toast.
- `Update` and the paint path also recover. One bug must never take the
  terminal down.
- **Message routing:**
  - `tea.KeyMsg` and `tea.MouseMsg` go to the top screen only.
  - Every other message goes to all screens, so background screens stay in
    sync. A screen must check that a message is for it (keys, generations).

**Async message types to know:**

| Message type | Purpose |
|---|---|
| `feedMsg` | home feed; `cached: true` means from disk, the live one replaces it |
| `searchMsg`, `detailsMsg`, `episodesMsg`, `variantsMsg`, `resolveMsg` | source pipeline |
| `posterMsg` | images |
| `dlMsg` | download manager changed (it pushes; no polling) |
| `malMsg` | MAL syncer status |
| `netMsg` | online/offline probe |
| `healthMsg` | source probe |
| `appSchedMsg` | AniList week schedule |
| `sugFireMsg` / `sugResMsg` | search-as-you-type |
| `dwellMsg` | episode prefetch |
| `similarMsg`, `malOpenMsg`, `subsMsg`, `subGotMsg` | feature flows |

### The play pipeline

```
search ─▶ results (merged across sources by folded title)
       ─▶ animeScreen: fetchDetails ─▶ seasons ─▶ fetchEpisodes
       ─▶ picker: fetchVariants (fansub × host) ─▶ resolve (≤3 hosts in parallel) ─▶ qualities
       ─▶ playing: aniskip.Fetch ─▶ player.Start(mpv, IPC) ─▶ progress events ─▶ history save
```

- **Auto-play:** `p` on an episode, or `picker.auto`, picks the preferred
  fansub (from history), then the first host that resolves.
- **Failover:** a failed playback marks `a.failed[url]`, then reopens the
  picker in auto mode with `attempt+1` (up to 3). A fansub whose hosts all
  fail moves to the next group (`picker.tried`).
- **Download mode:** `picker.dlMode` is 1 for one episode, 2 for a season.
  The same screen queues downloads instead of playing.
  `download.Request{Strict: true}` locks the chosen fansub.

---

## 4. The UI package

### 4.1 Screens

```go
type screen interface {
    update(a *App, msg tea.Msg) tea.Cmd
    view(a *App, w, h int) string   // body only; header/footer are App's
    crumb() string                  // breadcrumb label ("" = none)
    hints() []hint                  // footer keys, MOST IMPORTANT FIRST
}
// optional:
type animator interface{ animating(a *App) bool } // request frames while true
type typer    interface{ typing() bool }          // printable keys belong to a text field
```

| Screen | File | Notes |
|---|---|---|
| `home` | home.go, homemenu.go | hero logo, search box (+ `suggest.go`), one menu (devam et items + destinations) with a preview column |
| `feedScreen` | feedview.go | "Yeni bölümler": the live feed (data stays on home) |
| `results` | results.go | merged search results, detail pane |
| `animeScreen` | anime.go | seasons, episodes, multi-select (`v`), notes, dwell prefetch (`speed.go`), recovery (`r`) |
| `charactersScreen` | characters.go | AniList character & Japanese voice actor (`Seiyuu`) list, Sixel/halfblock portraits, spoiler toggle (`x`) |
| `picker` | picker.go | fansub/host/quality; play, single download, or batch/range download (`30-100`); stream URL inspector (`u`) |
| `playing` | playing.go, subsui.go | idle card; external subtitles |
| `library` | library.go | shelves, list/wall, MAL status line, graphical statistics view (`i`) |
| `discover` | discmenu.go, discover.go, tonight.go | section menu first (`d.menu`, cursor = `d.tab`), then one section page: `tCal tTonight tSeason tGenre tRecs tRandom` |
| `downloads` | downloads.go, dlib.go | offline shelf (default) / queue (`v`) with live ASCII speed sparkline |
| `offlineScreen` | offline.go | root when there is no internet |
| `settings` | settings.go | grouped (`settingGroup`), scrolls |
| `malScreen` | malui.go | MAL connect / sync |
| `sourcesScreen` | health.go | source health |
| `onboard` | onboard.go | first-run guide / "what's new" |

**Overlays** are App-level and drawn over the body:

- `palette` (palette.go, `ctrl+k`, fuzzy matching, live theme preview);
- `libPopup` (popup.go, status/score/lists);
- `dropdown` (dropdown.go, bell `◆` / burger `≡` / generic `openList`);
- `actions` (actions.go, `?`/`F1`): the screen's `hints()` plus global keys; choosing one replays its key (`replayKey`);
- help (help.go, the static key sheet; "bütün kısayollar" in işlemler).

While any overlay is open, sixel images are suppressed (see §5).

### 4.2 Frame composition

`App.compose()` returns the frame as styled lines: header, rule, body,
footer. Then:

1. The screen draws the body as strings, using the kit (`kit.go`:
   `pageHead`, `split`/`columns`, `menuRows`, `label`, `emptyState`; see
   DESIGN.md) and the helpers in `layout.go` and `theme.go`:
   - `row(w, bg, leftSpans, rightSpans)` — the standard list row;
   - `panel()` — a rounded box, the title set into the border (`panes` pairs two);
   - `block`, `hjoin`, `fit`, `truncate`, `wrap`, `center`,
     `progressBar`, `sectionTitle`, `scrollbar`, `pill`.
2. The selection is drawn the same way everywhere: `bg = cBand`,
   `bar = sp("▌ ", accent)`, bold title. A `glide` (fx.go) gives the
   spring-sliding band.
3. Effects are spliced in afterwards:
   - confetti and sparks (fx.go);
   - the `anchor()` marker on the selected row, which tells effects where to
     spawn.
4. Overlays are composited with `overlay`, `overlayAt` and `dim`.
5. The footer comes from `footerHints()` + `hintLine()`: the first three
   screen hints, then "? işlemler" and "esc geri". Every hint is also an
   entry in işlemler. So **list hints most important first, and give every
   action a hint**.

### 4.3 Theme

- **All colours are package variables** (`cBg cText cSub cMuted cFaint cBand
  cSurface cShade cAccent cAccent2 cSky cOK cWarn cErr cInk`) plus styles
  (`stText`, …). `applyTheme(Theme)` swaps them all.
- **Never hard-code a hex colour** in a screen; use the tokens or `mix()`.
- `cFaint` is for decoration only, never for text a user must read. Use
  `cMuted` for that.
- **Theme morph:** `a.tfade` interpolates two themes over ~400 ms
  (`stepTheme` on ticks).
- **Cover theme** (`cover.go`): when the top screen is about an anime, accents
  and bands are tinted with the poster's dominant colour. The **ground colour
  `cBg` is never tinted**, because sixel corners are painted with it.
- **Terminal background:** the frame sets `Bg`, emitted as OSC 11. `main`
  restores it with OSC 111 on exit.

### 4.4 Animation clock

- `tickMsg` arrives at 30 fps **only while `needsTick()` is true**. That
  covers toasts, transitions, poster fades, encodes in flight, theme morphs,
  confetti, glides, open overlays, and any screen whose `animating()` is
  true.
- An idle app costs zero CPU. Respect this: if you animate, make
  `animating()` return true only while it matters.
- Reduced motion (`!a.cfg.Motion`) must skip every animation, glide and
  sound tick.

### 4.5 Other UI subsystems

| Subsystem | Where | Essentials |
|---|---|---|
| Notifications | dropdown.go | `a.addNotice(notice{key, kind, text, action})`; de-duplicated by `key`; unread count on the bell |
| Toasts | `a.notify(text, kind)` | transient footer message; kinds `ok warn err` (err also plays the error sound) |
| Same anime across sources | same.go | `sameAnime` (MAL id, else folded titles), `a.recent()` (history, one entry per show), `a.siblings(an)`, `a.sibling(an)`. **Anything the user sees as "this anime" (shelf, palette, tonight, suggestions, new-episode notices, shelf delete) must go through these**, never `hist.Recent()` / a source key alone. Library: read with `a.libItem(an)` (key → MAL id with film/series guard → title; memoised per `lib.Rev()`), write with `a.libSet(an, fn)`; never `a.lib.Get(an.Key())` / `a.lib.Set` from UI code. Opening: `animeScreen.start` ranks the sources once (`rankSources`: switched-on first, then registry rank) and opens on the best; progress comes from the freshest season-1 entry across sources. Watched state: `a.watchView(an).done(ep)` = history ✓ **or** within the watched count of the MAL entry the episode belongs to (season chain, see Season sync), else the first season only |
| Season sync | seasonsync.go, `internal/seasons` | One site title can span several MAL entries (AnimeciX Bleach = 5). `wantAlign` builds the AniList chain when an anime opens (`alignMsg`); `a.alignment(an)` aligns the site's season counts to it. `bumpWatched` → `recordWatched` files an episode under its MAL entry: the own entry always, **other entries only after the user said yes once per title** (`cfg.SeasonSync`, asked by `maybeAskSeasonSync`; yes backfills from history). `canonical` refuses overflow past a finished entry and site seasons the chain lacks: nothing is written rather than a wrong count. `libAnime` caps the own entry's `EpCount` at MAL's. The anime screen's `r` menu corrects the match by hand (`match.Store.SetUser`, applied by `userMal`) |
| Undo | undo.go | `a.remember(label, func(a){restore})`; `ctrl+z` within 2 min. Use it instead of "press twice to confirm" |
| Offline | offline.go | `probeNet()` at start; offline → `stack = [offlineScreen]`; re-probe every 15 s; online → fresh home |
| Follow / new episodes | follow.go | feed × history/library → notices; home calendar from AniList |
| Onboarding | onboard.go | 8-step tour (`ob*` consts); first run, or Settings → Tanıtım turu (`startTour`, does not touch `SeenVersion`). `cfg.SeenVersion` vs `ui.Version`; **add a `whatsNew[version]` entry for every release** |
| PATH | pathui.go, pathx | `a.path` (`pathOps`). **Tests swap it for `fakePath` in `pathui_test.go`'s `init`: never call `pathx` from UI code directly** |
| Sounds | sound pkg + `playSound(a, k)` | volume `cfg.Sounds`; tests set it to 0 |
| Thumbnails | thumbs.go | mpv screenshots to `CacheDir/kareler/<key>.jpg`; posters are disk-cached in `kapaklar/` |

---

## 5. Rendering: `internal/tty` (read before touching output)

Bubble Tea v1 rewrites a whole line on any change, and a rewritten line
erases sixel images on it, so posters blinked. A first replacement based on
x/cellbuf drifted out of sync with the real terminal. `internal/tty` is the
fix, built so drift cannot persist:

- **Double buffering.** A `Frame` is a cell grid (`Cell{G, W, S Style,
  Img}`) parsed from the composed lines with `SetLines`.
  - SGR sequences become styles.
  - Every other escape sequence is dropped.
  - Widths come from `ansi.StringWidth`; grapheme clusters from uniseg.
- **Mailbox ("triple") buffering.**
  - `Writer.Submit(frame)` never blocks.
  - A goroutine renders the newest frame only, paced at ~60 fps.
  - Tests use `selfPaintSync` instead.
- **Diff (pure function).**
  - Every changed run starts with an absolute CUP. No relative moves, no REP,
    no LF tricks.
  - After a *risky* glyph (anything ≥ U+2000 except box drawing and block
    elements, or a multi-rune cluster) the cursor is re-anchored, and the
    right neighbour is rewritten too. A terminal that draws an icon two cells
    wide cannot shift the rest of the row.
  - Autowrap is off (`?7l`), so nothing scrolls.
  - Each frame is wrapped in synchronized output (`?2026h/l`).
- **Self-healing.**
  - 1.2 s after activity stops, and at least every 5 s during constant
    animation, every text cell is rewritten once.
  - `ctrl+l` triggers `Redraw()`, a clear plus full repaint.
- **Pictures are a layer.**
  - `Frame.Place(Image)` reserves cells. The text diff never writes there.
  - A picture is (re)sent only when it is new, moved, or uncovered.
  - Before sending, the text under it is reset to plain blanks; a stale wide
    glyph there could otherwise be destroyed later, taking pixels with it.
  - A removed picture's rect is rewritten as text.
  - A picture touching the last row is refused, because it could scroll.
  - Wide or risky glyphs at a picture's edge are blanked.
- **Placing a picture in UI code.** Call `a.posters.render(url, w, h, frame)`.
  - In sixel mode it returns `gfx.place()` placeholder lines. These are
    ground-coloured spaces whose first line carries an APC marker
    `\x1b_kz<id>\x1b\\`.
  - `buildFrame()` extracts the markers into `Image`s.
  - Encoding runs asynchronously (`gfx.request`); half-blocks show until it
    is ready.
- **Two paths.**
  - `View()` (string) is Bubble Tea's path. It is used by snapshot tests and
    by the `renderer: "klasik"` fallback, and goes through `gfx.finish`.
  - `paint()` is the default path.

**Guarantees are tested:**

- `tty/emu_test.go`:
  - `Emulator` replays the output;
  - `TestDiffFuzz` (`ANICLITR_FUZZ=150` for a long run);
  - `TestDiffSurvivesWidthDisagreement`, where the emulated terminal draws
    ♥/★/PUA two cells wide;
  - picture stability tests.
- `ui/e2e_test.go`, `TestEndToEndNoDrift`: a whole session through the real
  renderer into the emulator, checked cell by cell.

**Poster memory (v0.10.1, see docs/OPTIMIZATION.md).**

- A poster keeps only a ≤96 px master (`posterState.img`, ~56 KB) in RAM.
- The sixel path builds its 480 px source **on demand** in the encode worker (`fullImage`): it reads the cover back from the disk cache (`kapaklar/`) and caches the encoded sixel. `posterState.raw` (the file bytes) is kept only when the cover is not on disk.
- Caps:
  - `maxPosters` masters, least recently drawn first, after `keepFresh`; trimmed covers reload by themselves when drawn (`evicted` → `reload` → `takeReloads` in `Update`).
  - `encBudget` bytes of sixels; never the ones drawn lately.
  - Sized copies are cleared on resize.
- `TuneGC` (GOGC 50, 128 MB soft limit) runs at start. `trimMemory` returns memory to the OS when mpv starts and when leaving poster-heavy screens.
- Guarded by `TestPosterMemoryBudget` and `TestPosterTrimReloads`; `ANICLITR_MEM=1` runs `TestMemHeavySession` (150 covers, < 40 MB live heap).
- **Never keep decoded full-size pictures around.**

**Glyph rule.** Prefer glyphs that exist in Cascadia Mono, the default
Windows Terminal font. These are missing from it: ✦ ✧ ↺ ★ ⁕ ⌕. Check with
`fontTools`. ★ ☆ are used deliberately for scores; the renderer survives
them.

---

## 6. Sources

### 6.1 Contract (`internal/source/types.go`)

```go
type Provider interface {
    Info() Info                                     // ID, Name, Lang, Primary, Legacy
    Search(ctx, q string) ([]Anime, error)
    Details(ctx, Anime) (Anime, error)               // seasons, MalID, poster, description…
    Episodes(ctx, Anime, season int) ([]Episode, error)
    Variants(ctx, Anime, Episode) ([]Variant, error) // fansub × host
    Resolve(ctx, Variant) (*Stream, error)           // playable qualities (+referer, UA, subtitles)
}
// optional: Feeder (Latest feed), Prober (cheap health check)
```

Register in `init()` with `source.Register(...)`, and add a blank import in
`cmd/aniclitr/main.go` (and in `ui/snap_test.go`, so UI tests see the same
set). Fill `Info.Note`: settings and the sources screen show it. The UI never
names a source: colours (`sourceColor`) follow rank, roles come from
`Info`, and hints list sources from the registry. Ranking: primary, then Turkish, then English, then
legacy. Call `source.Clean(q)` on every query, because terminals can inject
NUL characters.

### 6.2 AnimeciX (primary)

- **Required header.** Send `x-e-h: =.a`. Without it,
  `/secure/titles/{id}` returns a **different** title, so every response is
  ID-verified.
- **Mirror:** mangacix.net.
- **Endpoints:**
  - `/secure/search/{q}`
  - `/secure/titles/{id}?titleId=` (gives `mal_id`, seasons)
  - `/secure/related-videos` (episodes)
  - `/secure/episode-videos-points` (fansub × host matrix)
  - `/secure/last-episodes?page=N` (feed; 4 pages ≈ one week)
  - `secure/best-video` (302 to the embed)
- **tau-video:** `/api/video/{id}` gives direct MP4s on Cloudflare
  (`*.cfd`). The first fetch of a file comes from the origin and is slow
  (~2–12 MB/s); cached copies are fast (30+ MB/s).
- **Seasons vs MAL.** AnimeciX bundles seasons into one title; MAL gives each
  season its own id. The season chain (`internal/seasons`, wired in
  `ui/seasonsync.go`) maps each site episode to its MAL entry.

### 6.3 Others

- **SonAnime** (`api.sonanime.com`, JSON): `/anime/search?q=`, then
  `/anime/link/{slug}` holds `mal_id` and every season's episodes with direct
  MP4 links (1080/720/480). Links expire (`links_expire_at`, ~1 day), so
  `Resolve` always refetches; one upload per episode (no fansub choice).
- **Animim** (`animim.app`, JSON): every season is its own title.
  `/api/episodes/{id}` lists `fanSubVideos` (per fansub: `videoUrls`, signed
  `.ass` subtitles). Video URLs are `/api/video-proxy/…` → 302 → CDN MP4.
  Subtitles go to mpv as `--sub-file` and to downloads with their own
  extension.
- **Switched-off sources** (`cfg.Sources`): opening a title whose best known
  source is off first searches the switched-on ones (`animeScreen.gate`,
  `findAltsIn(…, true)`); only when none has it does it open on the off
  one, marked (`offSrc`). A pasted link or "open on X" is explicit and skips
  this.

- **AnimeTR** (animetr.co, Laravel, no bot wall):
  - `/search-series?q=` is JSON. **Every season is its own series**, so a
    series has one "season" and its MAL id (from the page's ld+json) is
    exact.
  - `/seri/{slug}` lists all episodes in one page (no paging, even for One
    Piece). Skip `ep-item__link`s of other series (the next-season card).
  - `/izle/{slug}/bolum-N` carries `<script id="epspage-data">`: fansub ×
    players (`provider`, `embed_url`).
  - OK.ru → HLS via yt-dlp; Google Drive → `extract.Drive`. Vidmoly,
    Vidoza, HDVid, Voe links are often dead (failover handles it).
  - The **VIP player** (azelvid / asyaanimeleri.pw) is skipped on purpose:
    anti-debug trap plus encrypted source key. Do not reverse it.
- **Google Drive** (any source): stream links are tied to cookies set while
  resolving, so a bare URL gets 403. `extract.Drive` checks the file with
  yt-dlp, then returns the **page** as `Quality{Page: true}`; mpv opens it
  through its yt-dlp hook (`--ytdl-format` capped at the height) and
  downloads go through yt-dlp.
- **HiAnime:**
  - The search HTML has `flw-item` cards.
  - `/api/theme/episode/list/{num}`, then `/servers?episodeId=`, give a
    base64 embed URL.
  - The ZokoAnime embed's `window.__P` is base64(JSON XOR
    `"otaku-embed-v1"`) and yields HLS plus subtitles. The MAL id is in the
    URL.
  - Some titles are dub-only.
- **AnimeDepo (legacy):** the GitLab `dizin.json` index plus per-anime JSON.
  Players with a `DEAD_` prefix are skipped. Resolved via yt-dlp.
- **Down:** TürkAnime closed 2026-09-19. Sibnet often returns 403 from
  Turkish ISPs.
- **Not usable by design (checked 2026-11-04; do not work around):**
  - OpenAnime: `api.openani.me` answers non-browser clients with 401 "OpenAnime
    Vanguard".
  - Anizm (anizm.net/.pro, anizle): Cloudflare managed challenge
    (`Cf-Mitigated: challenge`) on every request.
  - Deokwave: search is open, but video sits behind a CSRF-bound
    `POST /api/v1/video/token`.
  - tranimaci.com: own "Güvenlik Doğrulaması" wall.
  - Unreachable from some Turkish networks: animex.club, animeprax.com.

### 6.4 Health

`source.Check(ctx, p)` uses `Prober` or a small search. The UI probes all
enabled sources at start and every 5 minutes. Header dots show the state;
details are on `sourcesScreen`.

---

## 7. Playback (`internal/player`)

- `FindMpv` prefers `mpv.exe`; `mpv.com` breaks the pipe IPC.
- **IPC:** a named pipe `\\.\pipe\aniclitr-mpv-<pid>-<n>` (Unix socket
  elsewhere).
  - It observes `time-pos`, `duration` and `pause`.
  - Progress events are throttled to about 2.5 per second.
- **Key bindings sent to mpv:**
  - `n` / `b` send `script-message aniclitr-next` / `-prev`;
  - `q` sends `aniclitr-quit`, which takes a resume screenshot and then
    quits.
- **Skip:** segments are armed once the duration is known. They come from
  `aniskip.ForDuration`, which picks one segment per family by the closest
  episode length.
- **End card:** at the ED start (else 90 s before the end), `show-text` shows
  the next episode.
- **Thumbnails:** `screenshot-to-file` once per minute and on `q`.
- **"Watched":** `watchedAt = 0.75` (playing.go). It drives the history ✓,
  the MAL watched count and the shelf "next" episode.
- **Watch time:** forward progress at roughly real speed feeds
  `hist.AddActivity`, which powers the stats.

---

## 8. Persistence (`internal/store`, config)

**Where data lives:**

- Data: `config.Dir()` = `%APPDATA%\aniclitr`, or `ANICLITR_HOME`.
- Cache: `config.CacheDir()` = the user cache dir, or `ANICLITR_HOME/cache`.

| File | Owner | Content |
|---|---|---|
| `config.json` | config | settings (see README) |
| `history.json` | store.History | per-anime progress, watched map, partial %, fansub/host preference, **Notes**, daily **Activity** |
| `library.json` | store.Library | items: status, score, lists, `Watched`, MAL bookkeeping (`MalDirty`, `MalSeen`, `NoMal`); `MalDeleted` queue; custom list names |
| `downloads.json` | download.Manager | jobs (path, state, `Strict`/`Want` fansub, sizes) |
| `mal.json` | mal.Client | OAuth tokens + user name |
| cache `kapaklar/` `kareler/` `anilist/` `akis.json` `altyazi/` | ui, anilist, subs | posters, resume frames, GraphQL responses, last feed, subtitles |

**Rules:**

- **Atomic writes.** Write via `config.WriteAtomic`.
- **Corrupt files are never overwritten.** They are copied to `.bozuk*` and
  logged.
- **Data migration.** The old `%APPDATA%\kaze` directory is renamed on first
  run.
- **Library is the MAL mirror.** Items that exist only on MAL have
  `Source == "mal"`. Opening one runs `openByMal`, which searches the sources
  and matches `mal_id` through Details. `Library.Set` *adopts* the
  `mal:<id>` item when the same MAL id appears under a real source key, so no
  duplicates are created.

---

## 9. External services

| Service | Package | Auth | Notes |
|---|---|---|---|
| MyAnimeList API v2 | `mal` | OAuth2 PKCE (plain) with the **user's own** client id | redirect `http://localhost:47615/callback`; local one-shot HTTP server; refresh 5 min before expiry |
| MAL sync | `malsync` | — | pull at start and every 15 min; push dirty items 2 s after a burst; a local unsent edit wins; deletions queued; titles without a MAL id are matched via AniList search |
| AniList GraphQL | `anilist` | none | ~350 ms polite gap; memory and disk cache with TTL; `FuzzyDateInt` for date filters |
| Jikan | — | — | **often unreachable from Turkish networks. Do not use it.** |
| AniSkip v2 | `aniskip` | none | `types[]=op&ed&mixed-op&mixed-ed&recap&episodeLength=0` |
| OpenSubtitles REST v1 | `subs` | user API key (`opensubtitles_api_key`) | search, then download link, then file in cache; added to mpv with `sub-add` |

---

## 10. Downloads (`internal/download`)

- **Queue:** 2 jobs at a time. `Updates` is a coalesced signal, which the UI
  turns into `dlMsg`.
- **Fresh links.** A job resolves its stream **when it starts**, because CDN
  links expire. A fixed `Variant` can be given; otherwise the preferred
  fansub is used.
- **`Strict`.** Only the chosen fansub's hosts are tried. A missing fansub
  fails with a clear message; a retry keeps `Want`.
- **Tools:**
  - HLS goes to yt-dlp (`--progress-template` parsed);
  - otherwise aria2c (16 connections, `--continue`, retries and timeouts;
    the readout `[#… DL: CN: ETA:]` is parsed);
  - otherwise a native HTTP downloader with Range resume.
- **Side files:** subtitles are saved next to the video with the same base
  name, so mpv auto-loads them. The cover is saved as `kapak.jpg` for the
  offline shelf.
- **Test hook:** `Preview(Job)` inserts a job for UI tests; `noTools` forces
  the native path.

---

## 11. Configuration and environment

| Env var | Effect |
|---|---|
| `ANICLITR_HOME` | isolated data and cache dir (tests, portable use) |
| `ANICLITR_REDUCED_MOTION` | no animation |
| `NO_COLOR`, `TERM=dumb` | plain mode (no posters or motion) |
| `WT_SESSION`, `COLORTERM` | force truecolor |
| `ANICLITR_LIVE=1` | enable network tests |
| `ANICLITR_SNAP_DIR`, `ANICLITR_SWEEP_DIR` | write `.ans` snapshots for visual review |
| `ANICLITR_FUZZ=n` | longer renderer fuzzing |

**Startup** (`main.go`):

1. Handle subcommands.
2. Check for a TTY.
3. Init the log.
4. Load config.
5. Probe the colour profile.
6. Probe sixel with a DA1 query. Windows Terminal uses a fixed 10×20 virtual
   cell; elsewhere `sixel.CellSize()`.
7. Set `ui.Version`.
8. Choose the renderer and start Bubble Tea with `WithoutRenderer` (default)
   or the classic one.
9. On exit, write `tty.Leave` and OSC 111.

---

## 12. Testing

```
go vet ./...
go test ./internal/...
ANICLITR_LIVE=1 go test ./internal/...
```

| Test | What it guards |
|---|---|
| `tty` fuzz / width-disagreement / mailbox / heal | renderer correctness |
| `ui/e2e_test.go` | whole session through the real renderer, no drift |
| `ui/sweep_test.go` | every screen and overlay at 80×24 … 200×56: no panic, no overflow, exact height |
| `ui/keystorm_test.go` | every advertised key plus a random storm on every screen: no internal error |
| `ui/showcase_test.go` | offline showcase data; frame cost; feature tests |
| `ui/features_test.go`, `offline_test.go`, `v08_test.go` | cover theme, notes, bell/menu, offline mode, fansub-locked downloads, onboarding, tonight, offline shelf |
| `mal`, `malsync`, `subs`, `download` | fake servers: OAuth, pagination, PATCH, sync conflicts, strict fansub, native download |
| live tests (`ANICLITR_LIVE`) | real AnimeciX, AniList, aria2c, headless mpv IPC |

**Visual review.** Write snapshots with `ANICLITR_SNAP_DIR` or
`ANICLITR_SWEEP_DIR`, then render the `.ans` files to PNG. The workflow so
far used a small PIL script that understands truecolor SGR and OSC 11. Look
at the PNGs. Width checks alone do not catch ugliness.

**Test hygiene (must keep):**

- `testApp()` sets `ANICLITR_HOME` to a temp dir, `cfg.Sounds = 0`,
  `cfg.DownloadDir = t.TempDir()`, and forces its own `cfg.Sources`. Never
  rely on, or write to, the user's real config, history or Videos.
- Do not execute commands returned by `Update` blindly in tests (`drain`
  follows them). Some re-probe the network, for example the 15 s
  `netRetryMsg`.

---

## 13. How to…

**Add a source.** Implement `source.Provider` (and optionally `Feeder` /
`Prober`) in `internal/source/<name>`, register it in `init` with an
`Info.Note`, and import it in `main.go` and `ui/snap_test.go`. No UI change is
needed. Verify with `aniclitr doctor`.

**Add a screen.**
1. Implement `screen`, plus `animator` and `typer` if needed.
2. Push it with `a.push(s)`.
3. Return `nil` from `animating` when idle.
4. Write `hints()` in priority order.
5. Lay it out with `panel` and `row`.
6. Add it to `sweep_test.go` and `keystorm_test.go`.

**Add a setting.**
1. Add a field to `config.Config` with a JSON tag and a default in
   `defaults()`.
2. Add a row in `newSettings()`, then place it in a group via
   `settingGroup()`.
3. Document it in the README table.

**Add a global key.** Use the switch in `App.Update` (KeyMsg). Check for
conflicts first; the current map is in `help.go` and the README.

Known traps:

- Text inputs use ctrl+a/e/k/u.
- Home's search box owns letters until `esc`.
- Bare Ctrl cannot be detected by terminals.
- ctrl+l is redraw, which is why the archive toggle is alt+a.

**Ship a release.**
1. Add `whatsNew["x.y.z"]` in `onboard.go` (it is both the in-app card and the release notes). Update README/TODO. Commit everything.
2. Run `.\scripts\release.ps1 x.y.z`. It runs vet and tests, bumps `var version` in `main.go`, commits, creates the annotated tag `vx.y.z` with the notes, and pushes.
3. `.github/workflows/release.yml` then:
   - tests on windows-latest;
   - cross-builds 6 targets with `-X main.version` and `-X aniclitr/internal/update.Repo=$GITHUB_REPOSITORY`;
   - writes `checksums.txt` and runs `gh release create`.

**Installed copy vs. build (why there are two).**

- `pathx.Add` copies the running exe to `pathx.InstallDir()` and puts *that* folder on PATH. A build folder an older version put on PATH is migrated at start (`migratePath`).
- Only the installed copy (`pathx.RunningInstalled`) replaces itself: `update.Install` downloads `aniclitr_<os>_<arch>[.exe]`, checks it against `checksums.txt`, renames the running exe to `.old` (allowed on Windows) and moves the new one in. `main` deletes `.old` on the next start.
- A dev build (`bin\`) only reports a newer release. `update.Repo` is empty in plain `go build` output unless set, so dev builds do not check at all.

---

## 14. Traps (each one happened)

- **A discarded command leaves a poster loading forever.**
  `a.posters.want(url)` marks the URL as loading and returns the fetch
  command. **Always return or append it.**
- **Rows beside a picture.** With the classic renderer they repaint the
  picture. With `tty` they are safe, but never let text be written *into* a
  picture's rect.
- **Raw control characters in source.** Write `"\x1b"` in Go source, never a
  literal ESC byte. Code generators and patch scripts have produced them
  before.
- **Repeated splicing.** `ansi.Truncate`/`TruncateLeft` re-copy escape codes;
  splicing many times into one line explodes its size. Cut each segment from
  the original once (`ansi.Cut`).
- **Turkish text.** `strings.ToUpper` produces "ÇEVIRI"; use
  `ToUpperSpecial(unicode.TurkishCase, …)`. Slicing bytes of "Çar" yields
  invalid UTF-8; slice runes.
- **Sixel cell size.** Windows Terminal maps sixel to a 10×20 cell regardless
  of font. Encode pictures as exact multiples, and never let one reach the
  last row.
- **Field/method name clashes on `App`** (`pop`, `frame`): pick distinct
  names.
- **AnimeciX without the `x-e-h` header** silently returns the wrong anime.
- **Tests touching real data.** An early test once wrote into the user's real
  `history.json`. A later test could have queued a real download into
  `~/Videos`. Keep tests isolated.
- **mpv's default `s` key** takes a screenshot into the working directory
  (`mpv-shot0001.jpg`). That is mpv's behaviour, not ours.
