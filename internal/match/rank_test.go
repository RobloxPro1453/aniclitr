package match

import "testing"

func TestRankTitleExactBeatsIsekaiNoise(t *testing.T) {
	q := "another"
	exact := RankTitle(q, "[EMBER] Another (2012) (Season 1 + OVA) [BDRip] [1080p Dual Audio HEVC]")
	raw := RankTitle(q, "[Shiniori-Raws] Another (BD 1920x1080 x265 10bit FLAC)")
	s01 := RankTitle(q, "[Poopoo] Another - S01 (BD 1080p HEVC Opus) [Dual Audio]")
	rezero := RankTitle(q, "[ToonsHub] ReZERO -Starting Life in Another World- S04E18 1080p")
	elf := RankTitle(q, "Yandere Dark Elf: She Chased Me All the Way From Another World!")
	mushoku := RankTitle(q, "Mushoku Tensei Jobless Reincarnation S03E06 Another Domestic Disaster 1080p")

	if exact != 100 || raw != 100 || s01 != 100 {
		t.Fatalf("expected all real Another releases to score 100, got exact=%d raw=%d s01=%d", exact, raw, s01)
	}
	if rezero >= 50 || elf >= 50 || mushoku >= 50 {
		t.Fatalf("unrelated long titles must be heavily penalized (<50): rezero=%d elf=%d mushoku=%d", rezero, elf, mushoku)
	}
}
