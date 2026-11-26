package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// SleepMode controls the bedside sleep timer during playback.
type SleepMode int

const (
	sleepOff SleepMode = iota
	sleepThisEp
	sleepTwoEps
	sleep30Min
	sleep60Min
	sleepSuspendPC
)

type sleepTimer struct {
	mode     SleepMode
	epsLeft  int
	deadline time.Time
}

func (s *sleepTimer) active() bool { return s.mode != sleepOff }

func (s *sleepTimer) cycle() string {
	s.mode = (s.mode + 1) % 6
	s.epsLeft = 0
	s.deadline = time.Time{}
	switch s.mode {
	case sleepThisEp:
		s.epsLeft = 1
	case sleepTwoEps:
		s.epsLeft = 2
	case sleep30Min:
		s.deadline = time.Now().Add(30 * time.Minute)
	case sleep60Min:
		s.deadline = time.Now().Add(60 * time.Minute)
	case sleepSuspendPC:
		s.epsLeft = 1
	}
	return s.label()
}

func (s *sleepTimer) label() string {
	switch s.mode {
	case sleepThisEp:
		return "Bu bölüm bitince dur"
	case sleepTwoEps:
		return fmt.Sprintf("%d bölüm sonra dur", max(1, s.epsLeft))
	case sleep30Min, sleep60Min:
		rem := int(time.Until(s.deadline).Minutes()) + 1
		if rem < 1 {
			rem = 1
		}
		return fmt.Sprintf("%d dk sonra dur", rem)
	case sleepSuspendPC:
		return "Bölüm bitince PC'yi uyut"
	default:
		return "Kapalı"
	}
}

func (s *sleepTimer) shortPill() string {
	switch s.mode {
	case sleepThisEp:
		return "☾ 1 bölüm"
	case sleepTwoEps:
		return fmt.Sprintf("☾ %d bölüm", max(1, s.epsLeft))
	case sleep30Min, sleep60Min:
		rem := max(1, int(time.Until(s.deadline).Minutes())+1)
		return fmt.Sprintf("☾ %d dk", rem)
	case sleepSuspendPC:
		return "☾ bitince uyut"
	default:
		return ""
	}
}

func (s *sleepTimer) expired() bool {
	return (s.mode == sleep30Min || s.mode == sleep60Min) && !s.deadline.IsZero() && time.Now().After(s.deadline)
}

// onEpisodeFinished decrements episode-based timers and reports whether playback
// should stop (and optionally suspend the OS).
func (s *sleepTimer) onEpisodeFinished() (stop bool, suspend bool) {
	switch s.mode {
	case sleepThisEp:
		s.mode = sleepOff
		return true, false
	case sleepTwoEps:
		s.epsLeft--
		if s.epsLeft <= 0 {
			s.mode = sleepOff
			return true, false
		}
		return false, false
	case sleepSuspendPC:
		s.mode = sleepOff
		return true, true
	case sleep30Min, sleep60Min:
		if s.expired() {
			s.mode = sleepOff
			return true, false
		}
	}
	return false, false
}

// suspendOS puts the computer into sleep mode cleanly (only when explicitly selected).
func suspendOS() {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Start()
	case "darwin":
		_ = exec.Command("pmset", "sleepnow").Start()
	default:
		_ = exec.Command("systemctl", "suspend").Start()
	}
}
