package system

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os/exec"
	"strconv"
)

// chimeRate is the test sound's sample rate; it is 16-bit stereo.
const chimeRate = 48000

// PlayChime plays the output's test sound through pw-cat: a soft tone on
// the left, then a higher one on the right, so both sides are heard.
func PlayChime(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "pw-cat", "--playback", "--raw", "--format", "s16",
		"--rate", strconv.Itoa(chimeRate), "--channels", "2", "--media-role", "Notification", "-")
	cmd.Stdin = bytes.NewReader(chime())
	if out, err := cmd.CombinedOutput(); err != nil {
		if msg := lastLine(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// chime is the test sound: 660 Hz on the left, a pause, 880 Hz on the
// right, each struck and dying away; little-endian s16 frames, left first.
func chime() []byte {
	const tone, pause = chimeRate * 35 / 100, chimeRate * 15 / 100
	var b bytes.Buffer
	frame := func(l, r float64) {
		binary.Write(&b, binary.LittleEndian, [2]int16{int16(l * math.MaxInt16), int16(r * math.MaxInt16)})
	}
	strike := func(freq float64, left bool) {
		for i := range tone {
			t := float64(i) / chimeRate
			env := min(t/0.01, 1) * math.Exp(-t*9) // a 10 ms attack, then down
			v := 0.3 * env * math.Sin(2*math.Pi*freq*t)
			if left {
				frame(v, 0)
			} else {
				frame(0, v)
			}
		}
	}
	strike(660, true)
	for range pause {
		frame(0, 0)
	}
	strike(880, false)
	return b.Bytes()
}
