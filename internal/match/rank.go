package match

import (
	"regexp"
	"strings"
)

var (
	reRankBrack = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	reRankTail  = regexp.MustCompile(`(?i)(?:\s+[-–—]\s+(?:s\d{1,2}|\d{1,4}\b).*|\bs\d{1,2}(?:e\d{1,4})?(?:[-~]e?\d{1,4})?\b.*|\bseason\s*\d+.*|\b\d{2}\s*[-~]\s*\d{2,3}\b.*|\b(?:2160p|4k|1080p|720p|480p|576p|1920x1080|1280x720|bdrip|bdremux|bdmv|bluray|blu-ray|web-?dl|webrip|dvdrip|hevc|x264|x265|h\.?264|h\.?265|av1|flac|10bit|8bit|aac|ddp[\d.]*|opus|dual[\s.-]?audio|multi[\s.-]?sub(?:s)?|batch|complete|uncensored|raw|ova)\b.*)`)
)

// CleanDisplayTitle strips release/group/episode noise from a torrent or anime
// title while keeping the core series name intact for relevance ranking.
func CleanDisplayTitle(title string) string {
	s := reRankBrack.ReplaceAllString(title, " ")
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = reRankTail.ReplaceAllString(s, "")
	s = strings.Trim(s, " -–—:+~|/\\")
	s = Fold(s)
	return strings.Join(strings.Fields(s), " ")
}

// RankTitle scores how closely any of titles matches query, placing exact and
// prefix matches strictly above incidental words inside long unrelated titles
// (e.g. searching "another" places "Another" above "Re:ZERO ... in Another World").
//
// Tiers:
//
//	100: exact series match
//	 86: clean prefix match ("another: the other", "another 2")
//	 72: tight whole-word match (<= +1 extra word)
//	 52: medium whole-word match (<= +3 extra words)
//	 24: incidental word match inside a long unrelated title
//	 14: substring match
//	  0: no match
func RankTitle(query string, titles ...string) int {
	nq := strings.Join(strings.Fields(Fold(query)), " ")
	if nq == "" {
		return 50
	}
	qWords := strings.Fields(nq)
	best := 0

	eval := func(candidate string) {
		if candidate == "" {
			return
		}
		sc := scoreSingleTitle(nq, qWords, candidate)
		if sc > best {
			best = sc
		}
	}

	for _, raw := range titles {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		// Evaluate both the cleaned release title and raw folded title, plus
		// pipe/slash-separated alternate titles ("Japanese | English").
		eval(CleanDisplayTitle(raw))
		eval(strings.Join(strings.Fields(Fold(raw)), " "))
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '|' || r == '/' }) {
			if c := CleanDisplayTitle(part); c != "" {
				eval(c)
			}
		}
	}
	return best
}

func scoreSingleTitle(nq string, qWords []string, nt string) int {
	if nt == nq {
		return 100
	}
	if strings.HasPrefix(nt, nq+" ") || strings.HasPrefix(nt, nq+":") || strings.HasPrefix(nt, nq+"-") {
		// Still distinguish "Another: The Other" (short) from "Another World With My Smartphone..." (long unrelated)
		extra := len(strings.Fields(nt)) - len(qWords)
		if extra <= 2 {
			return 88
		}
		if extra <= 4 {
			return 76
		}
		return 58
	}

	tWords := strings.Fields(nt)
	matched := 0
	for _, qw := range qWords {
		for _, tw := range tWords {
			if strings.Trim(tw, ":,-()[]!?.'\"") == qw {
				matched++
				break
			}
		}
	}
	if matched == len(qWords) {
		extra := len(tWords) - len(qWords)
		switch {
		case extra <= 1:
			return 74
		case extra <= 3:
			return 52
		default:
			return 24
		}
	}
	if strings.Contains(nt, nq) {
		return 14
	}
	return 0
}
