package desktop

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestParentOf(t *testing.T) {
	if got := parentOf(os.Getpid()); got != os.Getppid() {
		t.Fatalf("parentOf(self) = %d, want %d", got, os.Getppid())
	}
	if got := parentOf(-5); got != -1 {
		t.Fatalf("parentOf(-5) = %d, want -1", got)
	}
}

func TestReapInherited(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	pid := cmd.Process.Pid // nobody waits for it, as after a restart
	ReapInherited()
	for range 100 {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child %d was not reaped", pid)
}

// Apps the shell starts do not inherit its layer-shell preload.
func TestSpawnEnv(t *testing.T) {
	env := []string{"HOME=/home/x", "LD_PRELOAD=/opt/layer-shell.so", "PATH=/bin"}
	got := spawnEnv(env)
	if len(got) != 2 || got[0] != env[0] || got[1] != env[2] {
		t.Fatalf("spawnEnv = %q", got)
	}
}
