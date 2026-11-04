package sound

import "testing"

func TestWavShape(t *testing.T) {
	for k := range recipes {
		w := wav(k, .5)
		if string(w[:4]) != "RIFF" || string(w[8:12]) != "WAVE" || len(w) < 200 {
			t.Fatalf("bad wav for %d (%d bytes)", k, len(w))
		}
	}
}
