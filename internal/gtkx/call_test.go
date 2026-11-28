package gtkx

import "testing"

func TestCallDoesNotRunExpiredCallback(t *testing.T) {
	ran := false
	if _, err := Call(0, func() bool { ran = true; return true }); err == nil {
		t.Fatal("call returned without a running main loop")
	}
	// Drain the pending work as if the main loop had just recovered.
	drain(0)
	if ran {
		t.Fatal("expired callback ran after the main loop recovered")
	}
}
