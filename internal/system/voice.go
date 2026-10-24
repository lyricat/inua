package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// VoiceState is what the voice daemon is doing, for the bar and the OSD.
type VoiceState struct {
	Available bool // voxtype is installed and the daemon answers
	Status    string
	Model     string // the loaded model, as the daemon reports it
	Error     string // why it is not available, in a word
}

// VoiceModel is a model the user can download.
type VoiceModel struct {
	ID      string // voxtype's name: "small-int8" for sensevoice
	Engine  string // "sensevoice", "paraformer", "whisper" …
	Label   string
	Size    string // a human size, as well as we know it
	Ordered int    // the order the settings page lists them
}

// VoiceModels are the models the settings page offers, first-party ones
// that voxtype setup model downloads. SenseVoice first: it is what the
// daemon defaults to and the quickest to load.
var VoiceModels = []VoiceModel{
	{ID: "small-int8", Engine: "sensevoice", Label: "SenseVoice small (int8)", Size: "~230 MB", Ordered: 0},
	{ID: "small", Engine: "sensevoice", Label: "SenseVoice small", Size: "~900 MB", Ordered: 1},
	{ID: "paraformer-zh", Engine: "paraformer", Label: "Paraformer zh (int8)", Size: "~230 MB", Ordered: 2},
}

// Voice follows voxtype: the daemon's state file for what it is doing, its
// CLI to start and stop recordings, and its models directory for what is
// downloaded. When voxtype is not there, every call degrades to "off".
type Voice struct {
	mu sync.Mutex
	// last is what the last poll saw; the settings page reads it while it
	// is open and the download runs.
	last VoiceState

	statePath  string
	modelsPath string
}

// NewVoice follows voxtype. statePath and modelsPath are where the daemon
// writes; "" reads the daemon's own defaults ($XDG_RUNTIME_DIR and
// ~/.local/share).
func NewVoice(statePath, modelsPath string) *Voice {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = "/run/user/1000"
	}
	if statePath == "" {
		statePath = filepath.Join(dir, "voxtype", "state")
	}
	if modelsPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			data := os.Getenv("XDG_DATA_HOME")
			if data == "" {
				data = filepath.Join(home, ".local", "share")
			}
			modelsPath = filepath.Join(data, "voxtype", "models")
		}
	}
	return &Voice{statePath: statePath, modelsPath: modelsPath}
}

// Run reports the state until ctx ends, watching the daemon's state file
// and checking the daemon itself is up every few seconds.
func (v *Voice) Run(ctx context.Context, onChange func(VoiceState)) {
	read := func() VoiceState {
		st := v.poll()
		v.mu.Lock()
		v.last = st
		v.mu.Unlock()
		return st
	}
	st := read()
	onChange(st)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if next := read(); next != st {
				st = next
				onChange(st)
			}
		}
	}
}

// poll reads the daemon's state once.
func (v *Voice) poll() VoiceState {
	if _, err := exec.LookPath("voxtype"); err != nil {
		return VoiceState{Available: false, Status: "unavailable", Error: "voxtype is not installed"}
	}
	data, err := os.ReadFile(v.statePath)
	if err != nil {
		return VoiceState{Available: false, Status: "stopped", Error: "the daemon is not running"}
	}
	st := strings.TrimSpace(string(data))
	switch st {
	case "idle", "recording", "transcribing", "stopped", "canceling", "":
		if st == "" {
			st = "idle"
		}
		return VoiceState{Available: true, Status: st, Model: v.loadedModel()}
	}
	return VoiceState{Available: true, Status: st}
}

// loadedModel asks the daemon's status line for the model it has. It fails
// quietly: the state file is the source of truth for what it is doing.
func (v *Voice) loadedModel() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "voxtype", "status", "--format", "json", "--extended").Output()
	if err != nil {
		return ""
	}
	var st struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(out, &st) != nil {
		return ""
	}
	return st.Model
}

// State is the last state the poll saw.
func (v *Voice) State() VoiceState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.last
}

// Start asks the daemon to record.
func (v *Voice) Start() error { return v.record("start") }

// Stop ends the recording and transcribes it.
func (v *Voice) Stop() error { return v.record("stop") }

// Toggle starts a recording, or ends it.
func (v *Voice) Toggle() error { return v.record("toggle") }

// Cancel throws the recording away.
func (v *Voice) Cancel() error { return v.record("cancel") }

func (v *Voice) record(what string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "voxtype", "record", what).Run()
}

// ModelExists reports whether a model is downloaded.
func (v *Voice) ModelExists(id string) bool {
	if v.modelsPath == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(v.modelsPath, voxtypeModelDir(id)))
	return err == nil && info.IsDir()
}

// voxtypeModelDir is the directory a model downloads into. The ONNX
// engines (sensevoice, paraformer) name it engine-id; whisper's is the
// model name alone.
func voxtypeModelDir(id string) string {
	return id
}

// DownloadModel downloads a whisper or parakeet model with voxtype setup
// --download, its only scriptable path. The ONNX engines' models
// (sensevoice, paraformer) download only through voxtype's interactive
// setup; SetupModel opens that in a terminal.
func (v *Voice) DownloadModel(ctx context.Context, engine, id string, done func(line string)) error {
	if engine == "sensevoice" || engine == "paraformer" {
		return fmt.Errorf("%s models download through voxtype's interactive setup (voxtype setup model)", engine)
	}
	cmd := exec.CommandContext(ctx, "voxtype", "setup", "--download", "--model", id, "--quiet")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if done != nil {
		done(strings.TrimSpace(string(out)))
	}
	return err
}

// SetupModelCommand is the command line that opens voxtype's interactive
// model setup (the only path that downloads the ONNX engines' models) in
// the terminal the settings give; the view runs it.
func SetupModelCommand(term []string, dashE bool) *exec.Cmd {
	if dashE {
		return exec.Command(term[0], append(slices.Clone(term[1:]), "-e", "voxtype", "setup", "model")...)
	}
	return exec.Command(term[0], append(slices.Clone(term[1:]), "voxtype", "setup", "model")...)
}

// RestartDaemon restarts the user service, as after a model or config
// change. Without systemd it tells the daemon to reload by restarting
// itself over its CLI.
func (v *Voice) RestartDaemon() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", "--user", "restart", "voxtype.service").Run(); err != nil {
		return fmt.Errorf("systemctl --user restart voxtype.service: %v", err)
	}
	return nil
}
