// Package sound plays tiny UI sounds synthesised in memory (no files).
package sound

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// Kind is a UI sound.
type Kind int

const (
	Tick   Kind = iota // moving the cursor
	Select             // enter / toggle
	Done               // celebration
	Error              // something failed
)

const rate = 22050

type note struct {
	freq, dur, start float64 // Hz, seconds, seconds
}

var recipes = map[Kind][]note{
	Tick:   {{2400, .018, 0}},
	Select: {{880, .045, 0}, {1320, .06, .035}},
	Done:   {{784, .09, 0}, {988, .09, .07}, {1175, .09, .14}, {1568, .22, .21}},
	Error:  {{220, .09, 0}, {185, .12, .07}},
}

var (
	mu    sync.Mutex
	cache = map[[2]int][]byte{}
	last  time.Time
)

// wav renders a recipe at volume v (0..1) as a 16-bit mono WAV.
func wav(k Kind, v float64) []byte {
	ns := recipes[k]
	total := 0.0
	for _, n := range ns {
		total = math.Max(total, n.start+n.dur)
	}
	samples := make([]int16, int(total*rate)+1)
	for _, n := range ns {
		s0 := int(n.start * rate)
		cnt := int(n.dur * rate)
		for i := 0; i < cnt && s0+i < len(samples); i++ {
			t := float64(i) / rate
			// soft attack, exponential decay; a touch of the octave for sparkle
			env := math.Min(1, t/0.003) * math.Exp(-t/(n.dur*0.35))
			x := math.Sin(2*math.Pi*n.freq*t) + 0.25*math.Sin(4*math.Pi*n.freq*t)
			val := float64(samples[s0+i]) + x*env*v*9000
			samples[s0+i] = int16(math.Max(-32767, math.Min(32767, val)))
		}
	}
	var b bytes.Buffer
	data := len(samples) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*2))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data))
	binary.Write(&b, binary.LittleEndian, samples)
	return b.Bytes()
}

// Play plays k at volume 0..100 (non-blocking). Ticks are rate-limited so
// holding an arrow key does not buzz.
func Play(k Kind, volume int) {
	if volume <= 0 {
		return
	}
	mu.Lock()
	if k == Tick && time.Since(last) < 45*time.Millisecond {
		mu.Unlock()
		return
	}
	last = time.Now()
	key := [2]int{int(k), volume}
	w, ok := cache[key]
	if !ok {
		w = wav(k, float64(volume)/100)
		cache[key] = w
	}
	mu.Unlock()
	play(w)
}
