package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// InstallVoxtype puts the release in ~/.local/bin only when its checksum
// is the one carried, then sets up the model and the service.
func TestInstallVoxtype(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	calls := filepath.Join(home, "calls")
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	// The "release": a script that notes how it is run.
	release := []byte("#!/bin/sh\necho \"voxtype $*\" >>" + calls + "\n" +
		"[ \"$1\" = setup ] && [ \"$2\" = --download ] && echo '{\"event\":\"progress\",\"file\":\"m.bin\",\"bytes\":5,\"total\":10,\"pct\":50}' && echo '{\"event\":\"done\"}'\nexit 0\n")
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"systemctl $*\" >>"+calls+"\n"), 0o755)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v"+VoxtypeVersion+"/voxtype-"+VoxtypeVersion+"-linux-"+voxtypeVariant()) {
			http.NotFound(w, r)
			return
		}
		w.Write(release)
	}))
	defer srv.Close()
	old, oldSums := voxtypeReleases, voxtypeSums
	defer func() { voxtypeReleases, voxtypeSums = old, oldSums }()
	voxtypeReleases = srv.URL

	// A checksum that differs: refused, nothing in place.
	voxtypeSums = map[string]string{voxtypeVariant(): strings.Repeat("0", 64)}
	if err := InstallVoxtype(context.Background(), "", func(VoxtypeStep, float64) {}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("wrong sum: %v", err)
	}
	dest := filepath.Join(home, ".local", "bin", "voxtype")
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("installed a file whose checksum differs")
	}
	if left, _ := filepath.Glob(filepath.Join(home, ".local", "bin", ".voxtype-*")); len(left) > 0 {
		t.Errorf("left %v", left)
	}

	sum := sha256.Sum256(release)
	voxtypeSums = map[string]string{voxtypeVariant(): hex.EncodeToString(sum[:])}
	var steps []VoxtypeStep
	var modelDone float64
	if err := InstallVoxtype(context.Background(), "zh_CN", func(s VoxtypeStep, done float64) {
		if len(steps) == 0 || steps[len(steps)-1] != s {
			steps = append(steps, s)
		}
		if s == VoxtypeModelDownloading {
			modelDone = max(modelDone, done)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dest); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("not installed: %v %v", info, err)
	}
	if VoxtypePath() != dest {
		t.Errorf("VoxtypePath = %q, want %q", VoxtypePath(), dest)
	}
	got, _ := os.ReadFile(calls)
	engine, model := VoxtypeStartModel("zh_CN", strings.Contains(voxtypeVariant(), "onnx"))
	want := "voxtype setup --download --model " + model + " --activate --progress-format json\n" +
		"voxtype config set engine " + engine + "\nvoxtype config set " + engine + ".model " + model + "\n"
	if engine == "whisper" {
		want += "voxtype config set whisper.language auto\n"
	}
	want += "voxtype setup systemd\nsystemctl --user enable --now voxtype.service\n"
	if string(got) != want {
		t.Errorf("ran:\n%s\nwant:\n%s", got, want)
	}
	if len(steps) != 3 || steps[0] != VoxtypeDownloading || steps[2] != VoxtypeService {
		t.Errorf("steps %v", steps)
	}
	if modelDone != 0.5 {
		t.Errorf("the model's progress reached %v, want 0.5", modelDone)
	}
}

// A language's first model: SenseVoice for Chinese, Japanese and Korean
// where the build has it; Whisper otherwise, English's own for English.
func TestVoxtypeStartModel(t *testing.T) {
	for _, c := range []struct {
		lang  string
		onnx  bool
		model string
	}{{"zh_CN", true, "sensevoice-small"}, {"ja", true, "sensevoice-small"}, {"zh_TW", false, "base"}, {"", true, "base.en"}, {"fr", true, "base"}} {
		if _, got := VoxtypeStartModel(c.lang, c.onnx); got != c.model {
			t.Errorf("%q onnx=%v: %s, want %s", c.lang, c.onnx, got, c.model)
		}
	}
}

// The engines `voxtype info engines` marks compiled.
func TestCompiledEngines(t *testing.T) {
	out := "Transcription engines\n  compiled  whisper ● active\n            parakeet\n  compiled  sensevoice\n\nSwitch with: voxtype config set engine <NAME>\n"
	if got := compiledEngines(out); !slices.Equal(got, []string{"whisper", "sensevoice"}) {
		t.Errorf("%v", got)
	}
}

// voxtype hands its words to inua, unless the user has a post-process
// command of their own, and leaves notifying and its hotkey to inua; what
// is set already is not set again, nor the daemon restarted for it.
func TestConfigureVoxtype(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	keys := filepath.Join(home, "keys")
	os.Mkdir(keys, 0o755)
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	calls := filepath.Join(home, "calls")
	// A voxtype whose config is a file a key, with the shell's own `read`
	// rather than cat and printf, which a system need not have on PATH.
	os.WriteFile(filepath.Join(bin, "voxtype"), []byte(`#!/bin/sh
case "$2" in
get) { read -r v <"`+keys+`/$3"; } 2>/dev/null && echo "$v" || echo null ;;
set) echo "set $3" >>"`+calls+`"; echo "$4" >"`+keys+`/$3" ;;
esac
`), 0o755)
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"systemctl $*\" >>"+calls+"\n"), 0o755)
	want := VoxtypeHandsOver("/opt/inua's/inua", "default")
	if want != `'/opt/inua'\''s/inua' call -n 'default' voice type -` {
		t.Errorf("command %s", want)
	}
	run := func() string {
		os.Remove(calls)
		if _, err := ConfigureVoxtype(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(calls)
		return string(b)
	}
	if got := run(); got != "set output.notification.on_transcription\nset hotkey.enabled\nset output.post_process.command\nset output.post_process.fallback_on_empty\nsystemctl --user restart voxtype.service\n" {
		t.Errorf("first:\n%s", got)
	}
	if got := run(); got != "" {
		t.Errorf("again:\n%s", got)
	}
	// A command of the user's own stays; the rest is inua's still.
	os.WriteFile(filepath.Join(keys, "output.post_process.command"), []byte("/home/me/clean.sh\n"), 0o644)
	os.WriteFile(filepath.Join(keys, "hotkey.enabled"), []byte("true\n"), 0o644)
	if got := run(); got != "set hotkey.enabled\nsystemctl --user restart voxtype.service\n" {
		t.Errorf("own command:\n%s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(keys, "output.post_process.command")); string(b) != "/home/me/clean.sh\n" {
		t.Errorf("the user's command became %q", b)
	}
}

// voxtype 0.7 has no `config get` or `config set`: its settings are read
// and written in its config.toml, and the rest of the file (the comments,
// the order, the user's own settings and tables) stays as it was.
func TestConfigureVoxtypeFile(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	// A voxtype of 0.7: no `config get`, no `config set`.
	os.WriteFile(filepath.Join(bin, "voxtype"), []byte("#!/bin/sh\nexit 2\n"), 0o755)
	calls := filepath.Join(home, "calls")
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"systemctl $*\" >>"+calls+"\n"), 0o755)
	conf := filepath.Join(home, ".config", "voxtype", "config.toml")
	os.MkdirAll(filepath.Dir(conf), 0o755)
	before := `engine = "sensevoice"

[hotkey]
  enabled = true
  key = "HOME"

[output]
  mode = "clipboard"
  [output.notification]
    on_recording_start = false
    on_transcription = true
`
	os.WriteFile(conf, []byte(before), 0o644)
	command := VoxtypeHandsOver("/opt/inua/inua", "default")
	changed, err := ConfigureVoxtype(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("nothing changed")
	}
	want := `engine = "sensevoice"

[hotkey]
  enabled = false
  key = "HOME"

[output]
  mode = "clipboard"
  [output.notification]
    on_recording_start = false
    on_transcription = false

[output.post_process]
command = "` + strings.ReplaceAll(command, `"`, `\"`) + `"
fallback_on_empty = false
`
	if got, _ := os.ReadFile(conf); string(got) != want {
		t.Errorf("config.toml:\n%s\nwant:\n%s", got, want)
	}
	// Again: nothing to write, and no restart.
	os.Remove(calls)
	if changed, err := ConfigureVoxtype(context.Background(), command); err != nil || changed {
		t.Errorf("again: changed=%v err=%v", changed, err)
	}
	if b, _ := os.ReadFile(calls); len(b) != 0 {
		t.Errorf("again restarted voxtype: %s", b)
	}
	// A post-process command of the user's own stays; the rest is inua's.
	own := strings.Replace(before, "  mode = \"clipboard\"\n", "  mode = \"clipboard\"\n  [output.post_process]\n    command = \"/home/me/clean.sh\"\n", 1)
	os.WriteFile(conf, []byte(own), 0o644)
	if _, err := ConfigureVoxtype(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(conf)
	if !strings.Contains(string(got), "command = \"/home/me/clean.sh\"") || strings.Contains(string(got), command) {
		t.Errorf("the user's command was replaced:\n%s", got)
	}
	if !strings.Contains(string(got), "  enabled = false\n") {
		t.Errorf("the hotkey is still on:\n%s", got)
	}
}

// A model's files go, and only they: Whisper's file, another engine's
// folder, nothing for a name that would leave the models folder.
func TestDeleteVoxtypeModel(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "voxtype", "models")
	os.MkdirAll(filepath.Join(dir, "sensevoice-small"), 0o755)
	os.WriteFile(filepath.Join(dir, "sensevoice-small", "model.onnx"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "ggml-base.en.bin"), nil, 0o644)
	os.WriteFile(filepath.Join(data, "keep"), nil, 0o644)
	for _, m := range []VoxtypeModel{{Engine: "whisper", Name: "base.en", Arg: "base.en"}, {Engine: "sensevoice", Name: "small", Arg: "sensevoice-small"}} {
		if err := DeleteVoxtypeModel(m); err != nil {
			t.Fatal(err)
		}
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left %v", left)
	}
	for _, arg := range []string{"..", "../keep", "", "base.en"} {
		if DeleteVoxtypeModel(VoxtypeModel{Engine: "parakeet", Name: arg, Arg: arg}) == nil {
			t.Errorf("deleted %q", arg)
		}
	}
	if _, err := os.Stat(filepath.Join(data, "keep")); err != nil {
		t.Error("deleted what is not a model")
	}
	if mb, langs := voxtypeInfo("sensevoice", "small", "sensevoice-small"); mb != 239 || langs != "zh/en/ja/ko/yue" {
		t.Errorf("sensevoice small: %d %q", mb, langs)
	}
}
