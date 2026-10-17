package vmui

import (
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/vm"
)

// The Docker socket is the one host check whose reason has to carry a
// warning: the socket is there and this user cannot use it, and the fix —
// joining the docker group — would give everything running as that user what
// amounts to root on this host. The window explains that, shows the command
// for whoever decides they want it, and points at the two ways that do not
// widen the account's rights.
func TestDockerDeniedExplainsTheGroupInsteadOfOfferingIt(t *testing.T) {
	got := vmCheckReason("Docker", vm.ReasonDenied)
	for _, want := range []string{"docker group", "root", "gpasswd", "rootless Docker", "Podman"} {
		if !strings.Contains(got, want) {
			t.Errorf("the reason for a denied Docker socket does not say %q:\n%s", want, got)
		}
	}
	// Every other check keeps the plain sentence: the kvm group grants one
	// device, and its remedy is a button.
	if got := vmCheckReason("KVM", vm.ReasonDenied); got != "Installed, but your user may not use it." {
		t.Errorf("the reason for a denied KVM check = %q", got)
	}
	if got := vmCheckReason("Docker", vm.ReasonStopped); got != "Installed, but not running." {
		t.Errorf("the reason for a stopped Docker daemon = %q", got)
	}
}
