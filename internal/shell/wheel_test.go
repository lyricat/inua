package shell

import (
	"testing"
	"time"
)

// One notch is one step, stopping at the ends; notches faster than the
// compositor answers go on from the step asked, and a burst is thinned.
func TestWheelStep(t *testing.T) {
	items := []string{"1", "2", "3"}
	t0 := time.Unix(1000, 0)
	var w WheelStep
	step := func(current string, dir int, at time.Duration) string {
		next, ok := w.Next(items, current, dir, t0.Add(at))
		if !ok {
			return "-"
		}
		return next
	}
	if got := step("1", 1, 0); got != "2" {
		t.Errorf("down from 1: %s", got)
	}
	if got := step("1", 1, 100*time.Millisecond); got != "-" {
		t.Errorf("a notch in the same burst: %s", got)
	}
	// The compositor still says 1; the step goes on from 2.
	if got := step("1", 1, 200*time.Millisecond); got != "3" {
		t.Errorf("down again before the answer: %s", got)
	}
	if got := step("3", 1, 400*time.Millisecond); got != "-" {
		t.Errorf("past the end: %s", got)
	}
	// Long after, the compositor's word is taken again.
	if got := step("2", -1, 2*time.Second); got != "1" {
		t.Errorf("up from 2: %s", got)
	}
	if got := step("1", -1, 3*time.Second); got != "-" {
		t.Errorf("past the start: %s", got)
	}
	// Nothing current (no app in front): the first, or the last.
	w = WheelStep{}
	if got := step("", 1, 4*time.Second); got != "1" {
		t.Errorf("down from none: %s", got)
	}
	w = WheelStep{}
	if got := step("", -1, 5*time.Second); got != "3" {
		t.Errorf("up from none: %s", got)
	}
	if _, ok := w.Next(nil, "", 1, t0.Add(6*time.Second)); ok {
		t.Error("a step in nothing")
	}
}
