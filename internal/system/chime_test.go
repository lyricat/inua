package system

import (
	"encoding/binary"
	"testing"
)

// The test sound is heard on the left, then on the right, never both at
// once, and soft.
func TestChime(t *testing.T) {
	pcm := chime()
	frames := len(pcm) / 4
	if len(pcm)%4 != 0 || frames != chimeRate*85/100 {
		t.Fatalf("%d bytes", len(pcm))
	}
	var leftEnd, rightStart int
	var peak int16
	for i := range frames {
		l := int16(binary.LittleEndian.Uint16(pcm[i*4:]))
		r := int16(binary.LittleEndian.Uint16(pcm[i*4+2:]))
		if l != 0 && r != 0 {
			t.Fatalf("frame %d on both sides", i)
		}
		if l != 0 {
			leftEnd = i
		}
		if r != 0 && rightStart == 0 {
			rightStart = i
		}
		peak = max(peak, l, r, -l, -r)
	}
	if leftEnd == 0 || rightStart <= leftEnd {
		t.Errorf("left until %d, right from %d", leftEnd, rightStart)
	}
	if peak > 32767*3/10 || peak < 32767/10 {
		t.Errorf("peak %d", peak)
	}
}
