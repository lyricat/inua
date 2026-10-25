package shell

import (
	"slices"
	"time"
)

// WheelStep turns the wheel's notches over a row of things (the bar's
// workspaces, the dock's running apps) into steps to the next or the
// previous one, stopping at the ends. The compositor answers a step after
// the next notch has come, so a step goes on from the one asked last while
// that is recent; and a notch sooner than wheelGap after a step is
// dropped, so a fast flick or a touchpad's swipe does not run past
// several.
type WheelStep struct {
	asked string    // the last step's
	at    time.Time // when it was taken
}

const (
	wheelGap  = 150 * time.Millisecond
	wheelHold = 600 * time.Millisecond
)

// Next is where a notch goes in items from current (dir > 0 the next, < 0
// the previous), and whether it goes anywhere: not at an end, nor too soon
// after the last step. A current not in items starts from the end the
// notch points away from.
func (w *WheelStep) Next(items []string, current string, dir int, now time.Time) (string, bool) {
	if dir == 0 || len(items) == 0 || now.Sub(w.at) < wheelGap {
		return "", false
	}
	if now.Sub(w.at) < wheelHold && slices.Contains(items, w.asked) {
		current = w.asked
	}
	i := slices.Index(items, current)
	switch {
	case i < 0 && dir > 0:
		i = 0
	case i < 0:
		i = len(items) - 1
	case dir > 0:
		i++
	default:
		i--
	}
	if i < 0 || i >= len(items) || items[i] == current {
		return "", false
	}
	w.asked, w.at = items[i], now
	return items[i], true
}
