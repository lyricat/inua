package system

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// VoxtypeModel is a speech model voxtype can download for one of its
// engines: Name as voxtype lists it, Arg as its download and its config
// name it ("sensevoice-small" for SenseVoice's "small").
type VoxtypeModel struct {
	Engine, Name, Arg string
	Installed         bool
	MB                int    // its download, 0 when not known
	Langs             string // "zh/en/ja", "en"; "*" for many; "" when not known
}

// VoxtypeCatalog is what the program at hand can do: the engines compiled
// into it, in its order, their models, and the engine and model in use.
type VoxtypeCatalog struct {
	Engines       []string
	Models        map[string][]VoxtypeModel
	Engine, Model string // in use (Model is an Arg)
	// More is set when this is a build with Whisper alone and the release
	// has one with every engine for this processor (UpgradeVoxtype).
	More bool
}

// VoxtypeModels asks voxtype for its catalog.
func VoxtypeModels(ctx context.Context) (VoxtypeCatalog, error) {
	var c VoxtypeCatalog
	bin := VoxtypePath()
	if bin == "" {
		return c, errors.New("voxtype is not installed")
	}
	out, err := exec.CommandContext(ctx, bin, "info", "models", "--json").Output()
	if err != nil {
		return c, fmt.Errorf("voxtype info models: %w", err)
	}
	var raw struct {
		Engines map[string]struct {
			Models []struct {
				Name        string `json:"name"`
				DownloadArg string `json:"download_arg"`
				Installed   bool   `json:"installed"`
			}
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return c, err
	}
	c.Models = map[string][]VoxtypeModel{}
	for engine, e := range raw.Engines {
		for _, m := range e.Models {
			vm := VoxtypeModel{Engine: engine, Name: m.Name, Arg: m.DownloadArg, Installed: m.Installed}
			vm.MB, vm.Langs = voxtypeInfo(engine, m.Name, m.DownloadArg)
			c.Models[engine] = append(c.Models[engine], vm)
		}
	}
	if out, err = exec.CommandContext(ctx, bin, "info", "engines").Output(); err != nil {
		return c, fmt.Errorf("voxtype info engines: %w", err)
	}
	c.Engines = compiledEngines(string(out))
	if out, err := exec.CommandContext(ctx, bin, "config", "get", "engine").Output(); err == nil {
		c.Engine = strings.TrimSpace(string(out))
	}
	if out, err := exec.CommandContext(ctx, bin, "config", "get", c.Engine+".model").Output(); err == nil && c.Engine != "" {
		c.Model = strings.TrimSpace(string(out))
	}
	_, onnx := voxtypeSums[voxtypeVariant()]
	c.More = onnx && strings.Contains(voxtypeVariant(), "onnx") && len(c.Engines) == 1
	return c, nil
}

// voxtypeInfo is a model's size and languages (voxtypeModelInfo), by its
// name or its folder.
func voxtypeInfo(engine, name, arg string) (int, string) {
	for _, k := range []string{name, arg} {
		if i, ok := voxtypeModelInfo[[2]string{engine, k}]; ok {
			return i.MB, i.Langs
		}
	}
	return 0, ""
}

// VoxtypeStartSize is the download InstallVoxtype makes for lang, in MB:
// the program and its first model.
func VoxtypeStartSize(lang string) int {
	variant := voxtypeVariant()
	engine, arg := VoxtypeStartModel(lang, strings.Contains(variant, "onnx"))
	mb, _ := voxtypeInfo(engine, arg, arg)
	if strings.Contains(variant, "onnx") {
		return mb + 46
	}
	return mb + 19
}

// voxtypeModelsDir is where voxtype keeps its models.
func voxtypeModelsDir() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "voxtype", "models")
}

// DeleteVoxtypeModel removes a downloaded model's files: Whisper's
// ggml-NAME.bin, the other engines' folder, named as their download
// (voxtype has no command for it). Only a file or folder of that name in
// voxtype's models folder goes.
func DeleteVoxtypeModel(m VoxtypeModel) error {
	dir := voxtypeModelsDir()
	if dir == "" || m.Arg == "" || strings.ContainsAny(m.Arg, "/\\") || m.Arg == "." || m.Arg == ".." {
		return fmt.Errorf("no files of %s to delete", m.Name)
	}
	name := m.Arg
	if m.Engine == "whisper" {
		name = "ggml-" + m.Arg + ".bin"
	}
	path := filepath.Join(dir, name)
	if _, err := os.Lstat(path); err != nil {
		return fmt.Errorf("no files of %s in %s", m.Name, dir)
	}
	return os.RemoveAll(path)
}

// compiledEngines reads `voxtype info engines`: a line for each engine,
// marked "compiled" when this build has it.
func compiledEngines(out string) []string {
	var engines []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "compiled" && !slices.Contains(engines, f[1]) {
			engines = append(engines, f[1])
		}
	}
	return engines
}

// UseVoxtypeModel downloads m if it is not there, makes it the one voxtype
// uses, and restarts the daemon to load it. progress hears each file's
// share as it downloads (pct < 0 when its size is not known), from the
// goroutine it runs on.
func UseVoxtypeModel(ctx context.Context, m VoxtypeModel, progress func(file string, pct float64)) error {
	bin := VoxtypePath()
	if bin == "" {
		return errors.New("voxtype is not installed")
	}
	if err := downloadModel(ctx, bin, m.Arg, progress); err != nil {
		return err
	}
	if err := activateModel(ctx, bin, m.Engine, m.Name, m.Arg); err != nil {
		return err
	}
	return runStep(ctx, "systemctl", "--user", "restart", "voxtype.service")
}

// activateModel makes voxtype use a model: its engine, and the model as
// that engine's config names it (Moonshine's short name, the others' Arg).
// --activate does not do it for every engine (SenseVoice's download leaves
// the config as it was), so it is set here. A multilingual Whisper model
// listens for any language; an English one, English.
func activateModel(ctx context.Context, bin, engine, name, arg string) error {
	value := arg
	if engine == "moonshine" {
		value = name
	}
	sets := [][]string{{"engine", engine}, {engine + ".model", value}}
	if engine == "whisper" {
		lang := "auto"
		if strings.HasSuffix(arg, ".en") {
			lang = "en"
		}
		sets = append(sets, []string{"whisper.language", lang})
	}
	for _, kv := range sets {
		if err := runStep(ctx, bin, "config", "set", kv[0], kv[1]); err != nil {
			return fmt.Errorf("voxtype config set %s: %w", kv[0], err)
		}
	}
	return nil
}

// VoxtypeHandsOver is voxtype's post-process command that hands each
// transcription to inua (`inua call voice type -`), which types it as it
// types the AI engine's; the daemon then outputs nothing itself.
func VoxtypeHandsOver(inua, instance string) string {
	return shellQuote(inua) + " call -n " + shellQuote(instance) + " voice type -"
}

// ConfigureVoxtype has voxtype hand its transcriptions to inua (command,
// VoxtypeHandsOver), unless a post-process command of the user's own is
// set, and leaves to inua what inua does: no notification of the words
// (the card shows them), no hotkey of its own (inua's shortcut drives it,
// and voxtype's own reads keyboards it cannot open). It restarts the
// daemon when anything changed, and reports whether it did.
//
// voxtype 1.1 has `config get` and `config set`; older releases (0.7, which
// distributions still package) have neither, so their config.toml is read
// and written here, leaving the rest of the file as the user keeps it.
func ConfigureVoxtype(ctx context.Context, command string) (bool, error) {
	bin := VoxtypePath()
	if bin == "" {
		return false, errors.New("voxtype is not installed")
	}
	get, set := voxtypeConfig(bin)
	want := [][2]string{
		{"output.notification.on_transcription", "false"},
		{"hotkey.enabled", "false"},
	}
	now, err := get(ctx, "output.post_process.command")
	if err != nil {
		return false, err
	}
	if now == "" || strings.Contains(now, " voice type -") { // not the user's own
		want = append(want, [2]string{"output.post_process.command", command}, [2]string{"output.post_process.fallback_on_empty", "false"})
	}
	changed := false
	for _, kv := range want {
		v, err := get(ctx, kv[0])
		if err != nil {
			return changed, err
		}
		if v == kv[1] {
			continue
		}
		if err := set(ctx, kv[0], kv[1]); err != nil {
			return changed, err
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, runStep(ctx, "systemctl", "--user", "restart", "voxtype.service")
}

// voxtypeConfig reads and writes voxtype's settings: through its own
// `config get` and `config set` when it has them, else through its
// config.toml.
func voxtypeConfig(bin string) (get func(context.Context, string) (string, error), set func(context.Context, string, string) error) {
	probe, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hasCLI := exec.CommandContext(probe, bin, "config", "get", "hotkey.enabled").Run() == nil
	get = func(ctx context.Context, key string) (string, error) {
		if !hasCLI {
			return voxtypeFileGet(key)
		}
		out, err := exec.CommandContext(ctx, bin, "config", "get", key).Output()
		if err != nil {
			return "", fmt.Errorf("voxtype config get %s: %w", key, err)
		}
		v := strings.TrimSpace(string(out))
		if v == "null" { // unset
			v = ""
		}
		return v, nil
	}
	set = func(ctx context.Context, key, value string) error {
		if !hasCLI {
			if err := voxtypeFileSet(key, value); err != nil {
				return fmt.Errorf("voxtype's config.toml, %s: %w", key, err)
			}
			return nil
		}
		if err := runStep(ctx, bin, "config", "set", key, value); err != nil {
			return fmt.Errorf("voxtype config set %s: %w", key, err)
		}
		return nil
	}
	return get, set
}

// voxtypeFile is voxtype's config.toml: $XDG_CONFIG_HOME/voxtype/config.toml,
// else ~/.config/voxtype/config.toml, where voxtype looks for it.
func voxtypeFile() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "voxtype", "config.toml")
}

// voxtypeFileGet reads a setting of voxtype's config.toml, named as voxtype
// names it ("output.notification.on_transcription"); "" when the file or the
// setting is not there, which is voxtype's own default for the settings
// ConfigureVoxtype writes.
func voxtypeFileGet(key string) (string, error) {
	path := voxtypeFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var cfg map[string]any
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	var v any = cfg
	table, name := voxtypeTable(key)
	for _, part := range table {
		v, _ = v.(map[string]any)[part]
	}
	if m, ok := v.(map[string]any); ok {
		v = m[name]
	}
	switch v := v.(type) {
	case nil:
		return "", nil
	case bool:
		return strconv.FormatBool(v), nil
	case string:
		return v, nil
	default:
		return fmt.Sprint(v), nil
	}
}

// voxtypeFileSet writes a setting of voxtype's config.toml, in the table it
// belongs to, and changes nothing else of the file: its comments, its order
// and the user's own settings stay as they are.
func voxtypeFileSet(key, value string) error {
	path := voxtypeFile()
	if path == "" {
		return errors.New("no home directory")
	}
	// A config.toml that is a link (chezmoi, stow) is written where it
	// points: replacing the link would drop the user's own copy.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	table, name := voxtypeTable(key)
	text := voxtypePut(string(data), strings.Join(table, "."), name, voxtypeValue(value))
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config.toml-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// voxtypePut is the TOML in text with name set to value in the table
// ("output.post_process"; "" for the top level): the key replaced where it
// is, added to its table where the table is, else in a table of its own at
// the end.
func voxtypePut(text, table, name, value string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	// The table's own lines: the file's before its first header, else those
	// after its header up to the next one.
	start, end := 0, len(lines)
	if table != "" {
		start = -1
		for i, l := range lines {
			trimmed := strings.TrimSpace(l)
			if !strings.HasPrefix(trimmed, "[") {
				continue
			}
			if start >= 0 {
				end = i
				break
			}
			if voxtypeHeader(trimmed) == table {
				start, end = i+1, len(lines)
			}
		}
		if start < 0 {
			out := strings.Join(lines, "\n")
			if out != "" {
				out += "\n\n"
			}
			return out + "[" + table + "]\n" + name + " = " + value + "\n"
		}
	}
	key := name + " = " + value
	indent := ""
	for i := start; i < end; i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if indent == "" {
			indent = l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		}
		if voxtypeLineIs(l, name) {
			lines[i] = indent + key
			return strings.Join(lines, "\n") + "\n"
		}
	}
	// Its table is there without it: the key goes last in the table, before
	// the blank lines that close it.
	at := end
	for at > start && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	lines = append(lines[:at], append([]string{indent + key}, lines[at:]...)...)
	return strings.Join(lines, "\n") + "\n"
}

// voxtypeHeader is a TOML table header's name: "[output.post_process]" is
// "output.post_process", "[ a . b ]" is "a.b".
func voxtypeHeader(header string) string {
	inner, _, _ := strings.Cut(strings.TrimPrefix(header, "["), "]")
	parts := strings.Split(inner, ".")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ".")
}

// voxtypeValue is value as TOML writes it: the settings written here are
// booleans ("true", "false") and strings (a command). A command's own quotes
// and backslashes are escaped, a literal string being unable to hold a quote.
func voxtypeValue(value string) string {
	if value == "true" || value == "false" {
		return value
	}
	return strconv.Quote(value)
}

// voxtypeTable is a dotted setting's table and its key there:
// "output.notification.on_transcription" is on_transcription in
// [output.notification].
func voxtypeTable(key string) (table []string, name string) {
	parts := strings.Split(key, ".")
	return parts[:len(parts)-1], parts[len(parts)-1]
}

// voxtypeLineIs reports whether a line of the file sets name (TOML allows
// spaces around its "="), not a comment nor another key ending like it.
func voxtypeLineIs(line, name string) bool {
	key, _, ok := strings.Cut(strings.TrimSpace(line), "=")
	return ok && strings.TrimSpace(key) == name
}

// shellQuote quotes s for sh, which runs voxtype's post-process command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// downloadModel runs voxtype's download with --activate, reading its
// progress events (NDJSON, src/setup/progress.rs): "progress" with the
// file, its bytes and pct (null when the size is not known), then "done"
// or "error" with a message.
func downloadModel(ctx context.Context, bin, arg string, progress func(file string, pct float64)) error {
	cmd := exec.CommandContext(ctx, bin, "setup", "--download", "--model", arg, "--activate", "--progress-format", "json")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	var failed string
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		var ev struct {
			Event, File, Message string
			Pct                  *float64
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "progress":
			pct := -1.0
			if ev.Pct != nil {
				pct = *ev.Pct
			}
			progress(ev.File, pct)
		case "error":
			failed = ev.Message
		}
	}
	err = cmd.Wait()
	switch {
	case failed != "":
		return errors.New(failed)
	case err != nil:
		if msg := lastLine(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// UpgradeVoxtype puts voxtype's build with every engine in place of one
// with Whisper alone (VoxtypeCatalog.More), checked as InstallVoxtype
// checks it, and restarts the daemon on it. The models stay.
func UpgradeVoxtype(ctx context.Context, done func(float64)) error {
	variant := voxtypeVariant()
	sum, ok := voxtypeSums[variant]
	if !ok || !strings.Contains(variant, "onnx") {
		return fmt.Errorf("no voxtype build with every engine for %s", variant)
	}
	dest := voxtypeInstallPath()
	if dest == "" {
		return errors.New("no home directory")
	}
	url := fmt.Sprintf("%s/v%s/voxtype-%s-linux-%s", voxtypeReleases, VoxtypeVersion, VoxtypeVersion, variant)
	if err := fetchChecked(ctx, url, dest, sum, done); err != nil {
		return err
	}
	return runStep(ctx, "systemctl", "--user", "restart", "voxtype.service")
}

// VoxtypeStartModel is the model InstallVoxtype starts with for a language
// (i18n.Lang: "zh_CN", "ja" …; "" for English), as an engine and its Arg:
// SenseVoice for Chinese, Japanese and Korean when the build has it,
// Whisper's multilingual base otherwise, its English one for English.
func VoxtypeStartModel(lang string, onnx bool) (engine, arg string) {
	switch l, _, _ := strings.Cut(lang, "_"); {
	case onnx && (l == "zh" || l == "ja" || l == "ko"):
		return "sensevoice", "sensevoice-small"
	case l == "" || l == "en":
		return "whisper", "base.en"
	}
	return "whisper", "base"
}
