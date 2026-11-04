package sound

import (
	"syscall"
	"unsafe"
)

var (
	winmm     = syscall.NewLazyDLL("winmm.dll")
	playSound = winmm.NewProc("PlaySoundW")
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndMemory    = 0x0004
	sndNoStop    = 0x0010
)

func play(w []byte) {
	if playSound.Find() != nil {
		return
	}
	// PlaySound reads the buffer asynchronously; every buffer lives in the
	// package cache for the whole run, so the pointer stays valid
	playSound.Call(uintptr(unsafe.Pointer(&w[0])), 0, sndAsync|sndNoDefault|sndMemory)
}
