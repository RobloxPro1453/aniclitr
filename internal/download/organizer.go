package download

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"aniclitr/internal/match"
)

// ParsedRelease holds structured metadata extracted deterministically from an
// anime torrent / scene / fansub filename (zero ML models).
type ParsedRelease struct {
	Original   string
	Group      string
	Series     string
	Season     int
	Episode    int
	Resolution string
	IsMovie    bool
	Ext        string
}

var (
	reLeadGroup  = regexp.MustCompile(`^\[([^\]]{1,36})\]\s*`)
	reTrailGroup = regexp.MustCompile(`-([A-Za-z0-9][A-Za-z0-9_]{1,24})$`)
	reBrackMeta  = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	reSxEy       = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*[Ex](\d{1,4})\b`)
	reSeasonWord = regexp.MustCompile(`(?i)\b(?:Season|Sezon|S)\s*(\d{1,2})\b|\b(\d{1,2})(?:st|nd|rd|th)\s+Season\b`)
	reDashEp     = regexp.MustCompile(`(?:^|\s)[-–—]\s*(\d{1,4})(?:v\d)?(?:\s|$|\.|\[|\()`)
	reWordEp     = regexp.MustCompile(`(?i)\b(?:Ep(?:isode)?|Bölüm|Bolum|E)\s*\.?\s*(\d{1,4})(?:v\d)?\b`)
	reResTag     = regexp.MustCompile(`(?i)\b(2160p|4K|1080p|720p|480p|576p|1920x1080|1280x720)\b`)
	reReleaseCut = regexp.MustCompile(`(?i)\b(2160p|4k|1080p|720p|480p|576p|1920x1080|1280x720|web-?dl|webrip|bdrip|bdmv|bluray|blu-ray|remux|dvdrip|hdtv|amzn|cr|nf|at-x|x264|x265|h\.?264|h\.?265|hevc|av1|10bit|8bit|flac|aac|ddp[\d.]*|opus|ac3|dual[\s.-]?audio|multi[\s.-]?sub|raw|vostfr|turkish|subbed|dubbed)\b.*$`)
	reMovieTag   = regexp.MustCompile(`(?i)\b(movie|film|gekijouban)\b`)
)

// IsVideoExt reports whether ext (with leading dot) is a playable anime video format.
func IsVideoExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mkv", ".mp4", ".webm", ".avi", ".m4v":
		return true
	}
	return false
}

// ParseRelease extracts group, series title, season, episode, and resolution
// from raw fansub/scene/torrent names like:
//   - "ReZero.kara.Hajimeru.Isekai.Seikatsu.S04E18.1080p.AMZN.WEB-DL.DDP2.0.H.264.mkv"
//   - "[ToonsHub] ReZero kara Hajimeru Isekai Seikatsu S04E18 1080p AMZN WEB-DL.mkv"
//   - "[SubsPlease] Sousou no Frieren - 12 (1080p) [A1B2C3D4].mkv"
//   - "[Shiniori-Raws] Another (BD 1920x1080 x265 10bit FLAC)"
func ParseRelease(name string) ParsedRelease {
	base := filepath.Base(strings.TrimSpace(name))
	ext := filepath.Ext(base)
	stem := base
	if IsVideoExt(ext) || strings.EqualFold(ext, ".torrent") {
		stem = strings.TrimSuffix(base, ext)
	} else {
		ext = ""
	}

	p := ParsedRelease{Original: base, Season: 1, Ext: strings.ToLower(ext)}

	// 1. Resolution anywhere in the original stem.
	if m := reResTag.FindStringSubmatch(stem); m != nil {
		r := strings.ToLower(m[1])
		switch r {
		case "4k":
			p.Resolution = "2160p"
		case "1920x1080":
			p.Resolution = "1080p"
		case "1280x720":
			p.Resolution = "720p"
		default:
			p.Resolution = r
		}
	}

	// 2. Leading [Group] tag.
	work := stem
	if m := reLeadGroup.FindStringSubmatch(work); m != nil {
		p.Group = strings.TrimSpace(m[1])
		work = strings.TrimSpace(work[len(m[0]):])
	}

	// 3. Replace dots/underscores with spaces (except inside H.264 / DDP2.0 which are stripped later).
	spaced := strings.NewReplacer(".", " ", "_", " ").Replace(work)
	spaced = strings.Join(strings.Fields(spaced), " ")

	// 4. Season & Episode detection.
	cutAt := len(spaced)
	if loc := reSxEy.FindStringSubmatchIndex(spaced); loc != nil {
		if sn, err := strconv.Atoi(spaced[loc[2]:loc[3]]); err == nil && sn > 0 {
			p.Season = sn
		}
		if en, err := strconv.Atoi(spaced[loc[4]:loc[5]]); err == nil && en > 0 {
			p.Episode = en
		}
		cutAt = min(cutAt, loc[0])
	} else {
		if m := reSeasonWord.FindStringSubmatch(spaced); m != nil {
			snStr := m[1]
			if snStr == "" {
				snStr = m[2]
			}
			if sn, err := strconv.Atoi(snStr); err == nil && sn > 0 {
				p.Season = sn
			}
		}
		if loc := reDashEp.FindStringSubmatchIndex(spaced); loc != nil {
			if en, err := strconv.Atoi(spaced[loc[2]:loc[3]]); err == nil && en > 0 && en < 2000 {
				p.Episode = en
				cutAt = min(cutAt, loc[0])
			}
		} else if loc := reWordEp.FindStringSubmatchIndex(spaced); loc != nil {
			if en, err := strconv.Atoi(spaced[loc[2]:loc[3]]); err == nil && en > 0 && en < 2000 {
				p.Episode = en
				cutAt = min(cutAt, loc[0])
			}
		}
	}

	// Trailing -Group (scene style) when no leading [Group] was found.
	if p.Group == "" {
		if m := reTrailGroup.FindStringSubmatch(work); m != nil {
			cand := m[1]
			if !strings.EqualFold(cand, "dl") && !strings.EqualFold(cand, "x") && !strings.EqualFold(cand, "rip") {
				p.Group = cand
			}
		}
	}

	// 5. Clean series name.
	series := spaced[:cutAt]
	series = reBrackMeta.ReplaceAllString(series, " ")
	series = reReleaseCut.ReplaceAllString(series, "")
	series = reSeasonWord.ReplaceAllString(series, "")
	series = strings.Trim(series, " -–—:.,/|")
	series = strings.Join(strings.Fields(series), " ")
	if series == "" {
		series = strings.TrimSpace(reBrackMeta.ReplaceAllString(spaced, " "))
		series = strings.Join(strings.Fields(series), " ")
	}
	p.Series = safeName(series)
	p.IsMovie = p.Episode == 0 && reMovieTag.MatchString(spaced)
	return p
}

func compactAlphaNum(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(match.Normalize(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MatchSeriesFolder resolves rawSeries to the best matching existing folder in
// existingFolders or known title in libTitles (so "ReZero kara Hajimeru..."
// lands directly inside existing "Re Zero kara Hajimeru Isekai Seikatsu").
func MatchSeriesFolder(rawSeries string, existingFolders, libTitles []string) string {
	rawSeries = strings.TrimSpace(rawSeries)
	if rawSeries == "" {
		return "anime"
	}
	rawCompact := compactAlphaNum(rawSeries)
	bestName := ""
	bestScore := 0.0

	checkPool := func(pool []string, minScore float64) {
		for _, cand := range pool {
			cand = strings.TrimSpace(cand)
			if cand == "" {
				continue
			}
			cc := compactAlphaNum(cand)
			if cc == "" {
				continue
			}
			if cc == rawCompact {
				bestName = safeName(cand)
				bestScore = 1.0
				return
			}
			sc := match.Ratio(rawSeries, cand)
			if sc < 0.85 && len(rawCompact) >= 6 && len(cc) >= 6 {
				// Space-insensitive Dice similarity catches "ReZero" vs "Re Zero".
				if sc2 := match.Ratio(rawCompact, cc); sc2 > sc {
					sc = sc2
				}
			}
			if sc >= minScore && sc > bestScore {
				bestScore = sc
				bestName = safeName(cand)
			}
		}
	}

	// Prefer existing folders on disk first so episodes always stay together.
	checkPool(existingFolders, 0.78)
	if bestScore < 0.95 {
		checkPool(libTitles, 0.82)
	}
	if bestName != "" {
		return bestName
	}
	return safeName(rawSeries)
}

// FormatOrganizedFilename builds a clean aniclitr-standard episode filename:
// "S04E18 [ToonsHub] [1080p].mkv" (compatible with ScanDisk & offline library).
func FormatOrganizedFilename(p ParsedRelease) string {
	ext := p.Ext
	if ext == "" {
		ext = ".mkv"
	}
	var name string
	switch {
	case p.IsMovie:
		name = "Film"
	case p.Episode > 0:
		name = fmt.Sprintf("S%02dE%02d", max(1, p.Season), p.Episode)
	default:
		// Fallback for unnumbered extras/specials: keep cleaned base stem.
		stem := strings.TrimSuffix(p.Original, filepath.Ext(p.Original))
		return safeName(stem) + ext
	}
	if p.Group != "" {
		name += " [" + safeName(shortName(p.Group)) + "]"
	}
	if p.Resolution != "" {
		name += " [" + safeName(p.Resolution) + "]"
	}
	return name + ext
}

// OrganizeFile moves a downloaded video file (and any matching sidecar subtitles)
// into <rootDir>/<MatchedSeries>/<FormattedName>, never overwriting existing files.
func OrganizeFile(rootDir, srcPath, hintTitle string, libTitles []string) (dstPath string, parsed ParsedRelease, err error) {
	parsed = ParseRelease(filepath.Base(srcPath))
	if parsed.Series == "" || parsed.Series == "anime" {
		if hint := ParseRelease(hintTitle); hint.Series != "" {
			parsed.Series = hint.Series
			if parsed.Group == "" {
				parsed.Group = hint.Group
			}
			if parsed.Resolution == "" {
				parsed.Resolution = hint.Resolution
			}
		}
	}

	// Collect existing series folders inside rootDir.
	var folders []string
	if entries, rerr := os.ReadDir(rootDir); rerr == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				folders = append(folders, e.Name())
			}
		}
	}
	seriesDirName := MatchSeriesFolder(parsed.Series, folders, libTitles)
	parsed.Series = seriesDirName
	targetDir := filepath.Join(rootDir, seriesDirName)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return srcPath, parsed, err
	}

	fileName := FormatOrganizedFilename(parsed)
	dstPath = filepath.Join(targetDir, fileName)
	if strings.EqualFold(filepath.Clean(srcPath), filepath.Clean(dstPath)) {
		return dstPath, parsed, nil
	}
	// Never overwrite an existing episode file.
	if fileExists(dstPath) {
		ext := filepath.Ext(fileName)
		base := strings.TrimSuffix(fileName, ext)
		for i := 2; i < 100; i++ {
			cand := filepath.Join(targetDir, fmt.Sprintf("%s (%d)%s", base, i, ext))
			if !fileExists(cand) {
				dstPath = cand
				break
			}
		}
	}
	if err := moveFileSafe(srcPath, dstPath); err != nil {
		return srcPath, parsed, err
	}
	// Move matching sidecar subtitles (.ass, .srt, .vtt) alongside.
	srcBase := strings.TrimSuffix(srcPath, filepath.Ext(srcPath))
	dstBase := strings.TrimSuffix(dstPath, filepath.Ext(dstPath))
	for _, subExt := range []string{".tr.ass", ".tr.srt", ".ass", ".srt", ".vtt"} {
		if fileExists(srcBase + subExt) && !fileExists(dstBase+subExt) {
			_ = moveFileSafe(srcBase+subExt, dstBase+subExt)
		}
	}
	return dstPath, parsed, nil
}

// OrganizeLooseFiles tidies loose video files sitting directly in m.dir root
// (e.g. from older raw aria2c torrent drops) into matching series folders and
// registers them in the offline shelf via ScanDisk.
func (m *Manager) OrganizeLooseFiles(libTitles []string) int {
	m.mu.Lock()
	root := m.dir
	m.mu.Unlock()
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() || !IsVideoExt(filepath.Ext(e.Name())) {
			continue
		}
		src := filepath.Join(root, e.Name())
		// Skip files currently being written (.aria2 sidecar present).
		if fileExists(src + ".aria2") {
			continue
		}
		if _, _, err := OrganizeFile(root, src, "", libTitles); err == nil {
			moved++
		}
	}
	if moved > 0 {
		m.ScanDisk()
	}
	return moved
}

func moveFileSafe(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device fallback: stream copy to temp, rename atomically, then remove source.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = in.Close()
	_ = os.Remove(src)
	return nil
}
