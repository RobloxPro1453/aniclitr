# aniclitr — Design system

How every screen is laid out, and why. The rules live here. Their single
implementation is `internal/ui/kit.go` (layout pieces) and
`internal/ui/actions.go` (keys). To change how the app looks, change
those two files. Screens follow on their own.

---

## 1. The idea

A web app has 100 units of screen. A terminal has 10. If the web layout is
copied literally (dashboards, many boxes, a toolbar of shortcuts), the
result is a spreadsheet: crowded, with no hierarchy, and hard to read on
first sight.

aniclitr takes the web app's *manners* instead of its layout:

| Web habit | Terminal form here |
|---|---|
| A sidebar of destinations | The home screen is one vertical menu |
| Hover previews | The selected item is previewed beside the list |
| Pages with a title and a lede | `pageHead`: title plus one sentence on what the page is for |
| A "⋯" menu per item | `?` opens **işlemler**: everything this screen can do |
| Breadcrumbs | The header: `aniclitr ╱ keşfet › bu sezon` |
| Cards | Every region is a rounded panel with its name in the border |

It moves the way classic CLI tools do, one step at a time:
**menu → list → item → action**, and `esc` goes back exactly one step.

---

## 2. Rules

1. **One screen, one job.** A page answers one question. It has at most
   two regions: the main list and, when the terminal is wide enough, a
   detail column for the selected item.
2. **Step by step.** Deep features live one level down, not on the
   surface. Keşfet is a menu of six sections, and each section is a page
   of its own. Never show six tabs and their contents at once.
3. **Four keys are enough.** These always work:

   | Key | Does |
   |---|---|
   | `↑↓` | select |
   | `enter` | open or do |
   | `esc` | back one step |
   | `?` | işlemler |

   - The footer shows at most three more keys, the most important first.
   - Every other action is in işlemler, with its shortcut next to it, so
     the shortcuts are learnt by use, never required.
   - Old shortcuts keep working. Never remove a feature to make a screen
     quieter: move it into işlemler.
4. **Every region is a panel.** A list and its detail each sit in a
   rounded box with the name in the top border (`╭─ BÖLÜMLER ──╮`). The
   focused one takes the accent colour. An earlier borderless try looked
   scattered: you could not tell which text belonged to which region.
   Inside a panel, groups are separated by a small label (`DEVAM ET`).
5. **One gradient per page.** The page title is the one place that uses
   the brand gradient. The accent colour marks the selection (the band
   with `▌`) and live state. Everything else is text, sub or muted.
6. **Motion over colour.** The glide band, slide transitions, reveals and
   the hero logo stay. They carry the "toy" feeling. Every one of them has
   a reduced-motion path.
   - While a picture is on screen, the selection band jumps instead of
     gliding (`a.picsOn`). Each extra frame makes Windows Terminal redraw
     the image as well.
   - Slow frames (over 30 ms) and slow terminal writes (over 50 ms) are
     written to the log as "yavaş kare" and "yavaş terminal".

---

## 3. Anatomy

```
 aniclitr ╱ keşfet › bu sezon                 ↓ 1 %25  kaynaklar ● ● ●  │  ◆ 6  ≡
 ──────────────────────────────────────────────────────────────────────────────
   Bu sezon                                                    Sonbahar 2026
   bu sezon yayınlananlar, en sevilenler önde

 ▌ Frieren                             9.1     │   (detail column)
   Dandadan                            8.7     │
   …                                           │

  enter aç   a listeme ekle   t fragman   ? işlemler   esc geri
```

**Header**
- Left: the wordmark, then crumbs.
- Right, only what matters: download progress, source health as dots (the
  names are on "kaynak durumu"), the archive pill, the bell `◆ n` and the
  menu `≡`.

**pageHead**
- Two rows on tall terminals: the title, then the lede in muted text.
- One row (`Title · lede`) under 30 rows.
- An optional right-aligned meta: counts, the season, the source.

**Body**
- The page gutter is `kitPad` (2).
- Panels sit side by side with `kitGap` (1) between them. `panes(left, lw,
  right, rw, h)` builds the pair; each `pane` draws its content for the
  inner size (`w-4` × `h-2`).
- `split(w)` decides the columns:
  - under 100 columns: the list alone;
  - otherwise: a detail column of 36–56 cells.

**Footer**
- `footerHints()` shows the screen's first 2–3 hints, then
  `? işlemler`, then `esc geri` (not on the home screen).
- While a text field has focus, the işlemler key is shown as `F1`.
- `hintLine` drops screen hints from the end; işlemler and geri always
  stay.

---

## 4. The kit (`internal/ui/kit.go`)

| Piece | Use |
|---|---|
| `pageHead(title, sub, meta, w, h)` | top of a page whose body is not yet indented |
| `pageHeadIn(…, inner, h)` | the same, for screens that indent their body themselves |
| `pageHeadCol(…, cw, w, h)` | the same, for a centred column (settings, sources, MAL) |
| `label(s, focused)` | a section label, Turkish upper case |
| `split(w)` / `columns(l, lw, r, rw, h)` | list plus detail columns |
| `indent(lines)` | the page gutter |
| `mItem`, `menuRows`, `menuMove`, `menuFirst`, `menuRowOf` | step-by-step menus with group labels, a description column and a right meta |
| `emptyState(title, text, w, h)` | a short centred message instead of an empty page |
| `panel(…)` (library.go) | a rounded box with the label in the border; content `w-4` × `h-2` |
| `pane`, `panes(…)` | a list panel plus an optional detail panel |

The helpers in `layout.go` and `theme.go` are unchanged and still used
inside regions: `row`, `row2`, `block`, `hjoin`, `fit`, `truncate`,
`wrap`, `center`, `progressBar`, `pill`, `sp`/`span`.

---

## 5. Keys (`internal/ui/actions.go`)

- `?` (or `F1`) calls `openActions()`. The panel lists the screen's
  `hints()` under **BU EKRAN**, then `globalActions()` under **HER
  YERDE**, then "bütün kısayollar" (the old static help).
- Choosing an item replays its key with `replayKey(parseKey(h.key))`
  through the whole `Update`. Screens do not need any action code; their
  `hints()` list *is* their action list.
- `parseKey` understands:
  - named keys (`enter`, `tab`, `space`, `pgdown`, `f1`…);
  - `ctrl+x` and `alt+x`;
  - single characters.
- A compound key such as `tab [ ]` runs its first key.
- Movement pairs (`← →`) and ranges (`1-6`) are shown, but cannot be run.

**Writing `hints()`**
1. Put the most important first. The footer shows the first three.
2. Give every action a hint, even a rare one. An action without a hint
   cannot be found.
3. Keep labels short and in Turkish, as verbs ("kitaplığa ekle").

---

## 6. Screens and where they sit

```
home (menu)
 ├─ devam et items        → anime → picker → playing
 ├─ Yeni bölümler         → feedScreen (feedview.go)
 ├─ Kitaplık              → library
 ├─ Keşfet                → discover: section menu (discmenu.go)
 │                             └─ section page (discover.go, tonight.go)
 ├─ İndirilenler          → downloads (offline shelf / queue)
 ├─ Ayarlar               → settings → MAL · kaynaklar · tur
 └─ Çıkış
search box (top of home)  → results → anime …
```

**Home** (`homemenu.go`)
- Logo, search, then up to three panels:
  - **ana menü**: up to five "devam et" items and the screens;
  - the preview of the selected item;
  - on wide terminals (≥ 150), a column with this week's calendar and
    the stats.
- The block is centred vertically on tall terminals.
- The preview column shows what the item would open:
  - a continue item: the resume frame or cover, the episode and progress;
  - Yeni bölümler: the latest episodes;
  - Kitaplık: the week's stats;
  - Keşfet: this week's calendar;
  - also downloads and settings.
- The feed, calendar and stats moved off the home surface. None of them
  were removed.

**Keşfet**
- `discover.menu` is true when it opens.
- Türler has one more step: `d.gpick` shows the genre list and the sort
  (`m`) first; `enter` opens that genre's list and `esc` comes back to
  the genre list.
- The menu cursor *is* `d.tab`, so the peek column and prefetching use the
  section code as it is.
- `enter` or `1-6` opens a section. `esc` comes back to the menu.
- `tab` and `shift+tab` still step between sections.

---

## 7. Changing the design next time

- **Spacing, gutters or column widths:** `kitPad`, `kitGap`, `split`.
- **Title style:** `pageHeadIn`.
- **Section look:** `label`, `panel`.
- **Menu rows:** `menuRows`.
- **Footer policy:** `footerHints`, `hintLine`.
- **Colours:** theme tokens (ARCHITECTURE.md §4.3). Never hard-code a hex
  value in a screen.
- **A new page:**
  1. Start with `pageHead`.
  2. Put the list on the left with `row` or `menuRows`.
  3. Add a detail column with `split`/`columns`.
  4. Write `hints()` in priority order.
  5. Add the page to `sweep_test.go` and `keystorm_test.go`.

**Checks**
- `TestScreenSweep` (sizes, widths) and `TestKeyStorm` cover every screen.
- `design_test.go` guards the key rules:
  - `parseKey`;
  - işlemler running the chosen key;
  - Keşfet's two steps;
  - a short footer;
  - the home menu reaching every screen.
- Visual review: `ANICLITR_SWEEP_DIR=… go test ./internal/ui -run
  TestScreenSweep`, then render the `.ans` files to PNG and look at them.
