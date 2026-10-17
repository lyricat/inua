// Command inua is the shell's command line: it controls a running shell and
// inspects Hyprland. The GTK4 shell itself is cmd/inua-shell.
//
//	inua call [-n instance] <target> <function> [args...]   control a running shell
//	inua show [-n instance]                                  list what call accepts
//	inua hypr dump | watch                           inspect Hyprland state
//	inua config check [path] | path                  check the settings file
//	inua config hyprland [path]                      check that Hyprland starts inua-shell and sources inua.conf
//	inua config apply                                write hypr/inua.conf from [input] and reload Hyprland
//	inua init [-force] [-pinentry ask|replace|keep] [-dir dir]  write the sample configuration
//	inua keyring status|import|enable|disable|replace|lock|paths|allow|disallow  the Secret Service kept in pass
//	inua workshop list|new|install|remove|run|dev    the light apps users write
//	inua mcp                                         inua as an MCP server on stdio, for an AI agent
//	inua upgrade [-check] [-force] [-version vX.Y.Z] install the latest release
//	inua terminal [command [args...]]               open the default terminal, running command
//
// The instance name defaults to $INUA_INSTANCE, else "default".
package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	samples "github.com/lyricat/inua/config"
	"github.com/lyricat/inua/internal/bootstrap"
	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/ipc"
)

// version is the release, set by the release build (-X main.version).
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "call", "show":
		err = cmdControl(os.Args[1], os.Args[2:])
	case "clipboard":
		if len(os.Args) < 3 || len(os.Args) > 4 || os.Args[2] != "capture" {
			usage()
		}
		err = cmdClipboardCapture(os.Args[3:])
	case "hypr":
		err = cmdHypr(os.Args[2:])
	case "config":
		err = cmdConfig(os.Args[2:])
	case "init":
		err = cmdInit(os.Args[2:])
	case "keyring":
		err = cmdKeyring(os.Args[2:])
	case "vm":
		err = cmdVM(os.Args[2:])
	case "workshop":
		err = cmdWorkshop(os.Args[2:])
	case "mcp":
		err = cmdMCP()
	case "upgrade":
		err = cmdUpgrade(os.Args[2:])
	case "terminal":
		err = cmdTerminal(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("inua", version)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "inua:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  inua call [-n instance] <target> <function> [args...]
  inua show [-n instance]
  inua hypr dump|watch
  inua config check [path] | path
  inua config hyprland [path]
  inua config apply
  inua init [-force] [-pinentry ask|replace|keep] [-dir dir]
  inua keyring status|import [-n]|enable|disable|replace|lock
  inua keyring paths | allow PATTERN | disallow PATTERN
  inua workshop list|new [--js] DIR [NAME]|install [-y] URL|DIR|remove ID|run ID|DIR|dev DIR
  inua vm list|doctor|status [--json]
  inua mcp
  inua upgrade [-check] [-force] [-version vX.Y.Z]
  inua clipboard capture [history-path]
  inua terminal [command [args...]]
  inua version`)
	os.Exit(2)
}

// cmdTerminal opens the default terminal (Settings → Default Apps: the
// launcher's terminal, else $TERMINAL, xdg-terminals.list's,
// xdg-terminal-exec), running command in it if given.
func cmdTerminal(command []string) error {
	cfg, _, _ := config.Load(config.DefaultPath()) // a broken file leaves the defaults
	argv, dashE := desktop.Terminal(cfg.TerminalCommand())
	if len(command) > 0 && dashE {
		argv = append(argv, "-e")
	}
	argv = append(argv, command...)
	if _, err := exec.LookPath(argv[0]); err != nil {
		return fmt.Errorf("no terminal to open (%s): choose one in Settings, Default Apps", argv[0])
	}
	return desktop.Spawn(argv, "")
}

// defaultInstance names the control socket when -n is not given.
var defaultInstance = cmp.Or(os.Getenv("INUA_INSTANCE"), "default")

// cmdControl sends one request to a running shell's control socket: op
// "call" with a target, function and arguments, or "show".
func cmdControl(op string, args []string) error {
	fs := flag.NewFlagSet(op, flag.ExitOnError)
	instance := fs.String("n", defaultInstance, "instance name")
	fs.Parse(args)
	rest := fs.Args()
	req := ipc.Request{Op: op}
	switch {
	case op == "show" && len(rest) == 0:
	case op == "call" && len(rest) >= 2:
		req.Target, req.Function, req.Args = rest[0], rest[1], rest[2:]
	default:
		usage()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := ipc.Call(ctx, ipc.SocketPath(*instance), req)
	if err != nil {
		return err
	}
	if resp.Result != "" {
		fmt.Println(resp.Result)
	}
	return nil
}

// printer is a hypr.Handler that prints to stdout.
type printer struct{ events bool }

func (p printer) OnSnapshot(s hypr.Snapshot) {
	fmt.Printf("== snapshot: focused monitor %q, active toplevel %q\n", s.FocusedMonitor, s.ActiveToplevel)
	for _, m := range s.Monitors {
		fmt.Printf("monitor   %-10s id=%d %dx%d@%d,%d focused=%v ws=%d\n", m.Name, m.ID, m.Width, m.Height, m.X, m.Y, m.Focused, m.ActiveWorkspaceID)
	}
	for _, w := range s.Workspaces {
		fmt.Printf("workspace %-10s id=%d mon=%s active=%v focused=%v fullscreen=%v urgent=%v\n", w.Name, w.ID, w.Monitor, w.Active, w.Focused, w.HasFullscreen, w.Urgent)
	}
	for _, t := range s.Toplevels {
		fmt.Printf("toplevel  %s ws=%d mon=%s active=%v urgent=%v %s: %q\n", t.Address, t.WorkspaceID, t.Monitor, t.Activated, t.Urgent, t.Class, t.Title)
	}
}

func (p printer) OnEvent(e hypr.Event) {
	if p.events {
		fmt.Printf("event     %s>>%s\n", e.Name, e.Data)
	}
}

func (p printer) OnConnection(up bool, err error) {
	fmt.Printf("== connected=%v err=%v\n", up, err)
}

func cmdHypr(args []string) error {
	if len(args) != 1 {
		usage()
	}
	paths, err := hypr.PathsFromEnv()
	if err != nil {
		return err
	}
	client := hypr.NewClient(paths)
	switch args[0] {
	case "dump":
		q, err := client.Query(context.Background())
		if err != nil {
			return err
		}
		s, err := hypr.BuildSnapshot(q, nil)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	case "watch":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		hypr.NewStore(client, printer{events: true}).Run(ctx)
		return nil
	}
	usage()
	return nil
}

// cmdClipboardCapture adds the selection on stdin to the clipboard
// history; the shell runs it through wl-paste --watch. It reads the
// settings each time, so a new limit, or recording turned off, holds
// from the next copy.
func cmdClipboardCapture(args []string) error {
	path := clipboard.HistoryPath()
	if len(args) == 1 {
		path = args[0]
	}
	c, _, _ := config.Load(config.DefaultPath()) // a broken file leaves the defaults
	if !c.Clipboard.Enable {
		io.Copy(io.Discard, os.Stdin)
		return nil
	}
	from := clipboard.FindSource(focusedWindow(), c.Clipboard.SensitiveSources)
	if clipboard.MarkedSensitive(offeredTypes()) {
		from.Sensitive = true // a password manager's secret
	}
	if from.Sensitive {
		io.Copy(io.Discard, os.Stdin) // and Capture keeps nothing
	}
	return clipboard.Capture(path, c.Clipboard.Limit, from, os.Stdin)
}

// offeredTypes are the types the clipboard's owner offers now; none when
// wl-paste cannot say in time.
func offeredTypes() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wl-paste", "--list-types").Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// focusedWindow is the window focused now, as Hyprland names it; none
// without Hyprland.
func focusedWindow() clipboard.Window {
	var w clipboard.Window
	paths, err := hypr.PathsFromEnv()
	if err != nil {
		return w
	}
	c := hypr.NewClient(paths)
	c.Timeout = 300 * time.Millisecond // a copy is not held up for long
	data, err := c.Request(context.Background(), "j/activewindow")
	if err == nil {
		json.Unmarshal(data, &w)
	}
	return w
}

// cmdConfig checks the settings file the way inua-shell reads it.
func cmdConfig(args []string) error {
	if len(args) == 0 {
		usage()
	}
	path := config.DefaultPath()
	switch {
	case args[0] == "path" && len(args) == 1:
		fmt.Println(path)
		return nil
	case args[0] == "check" && len(args) <= 2:
		if len(args) == 2 {
			path = args[1]
		}
	case args[0] == "hyprland" && len(args) <= 2:
		hypr := hyprConfig(path)
		if len(args) == 2 {
			hypr = args[1]
		}
		return checkHyprland(hypr)
	case args[0] == "apply" && len(args) == 1:
		return applyHyprland(path)
	default:
		usage()
	}
	return checkConfig(path)
}

// hyprConfig is the config Hyprland reads next to the settings file at
// path: hyprland.lua when the running Hyprland runs it (or there is no
// hyprland.conf), else hyprland.conf.
func hyprConfig(path string) string {
	runsLua := false
	if paths, err := hypr.PathsFromEnv(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		runsLua = hyprconf.RunsLua(ctx, hypr.NewClient(paths))
		cancel()
	}
	return hyprconf.ConfigFile(filepath.Dir(hyprconf.Path(path)), runsLua)
}

// checkHyprland reports whether a Hyprland config (or a file it sources)
// starts inua-shell, and whether it loads inua's file (inua.conf, or
// inua.lua for hyprland.lua). Both the Inua and
// the Hyprland session read this config, and inua init never changes an
// existing one, so without the first line neither session shows inua.
func checkHyprland(path string) error {
	where, err := bootstrap.FindShellStart(path)
	switch {
	case err == nil:
		fmt.Printf("%s: starts inua-shell (%s)\n", path, where)
	case errors.Is(err, bootstrap.ErrNoShell) && strings.HasSuffix(path, ".lua"):
		fmt.Printf("%s does not start inua-shell, so the Inua session would show no bar,\n"+
			"launcher or wallpaper. Add this line to it:\n\n    %s\n\n", path, bootstrap.LuaShellLine)
		return fmt.Errorf("%s does not start inua-shell", path)
	case errors.Is(err, bootstrap.ErrNoShell):
		fmt.Printf("%s does not start inua-shell, so the Inua session would show no bar,\n"+
			"launcher or wallpaper. inua init leaves an existing Hyprland config alone;\n"+
			"add this line to it:\n\n    %s\n\n"+
			"and, if you want them, inua's key bindings and layer rules from the sample\n"+
			"(inua init -force replaces the file with the sample, keeping yours as .bak).\n",
			path, bootstrap.ShellLine)
		return fmt.Errorf("%s does not start inua-shell", path)
	case err != nil:
		return err
	}
	checkSource(path, hyprconf.Target(path))
	return nil
}

// checkSource reports whether a Hyprland config loads inua's file target,
// and after the settings inua writes there. Without it inua's keyboard and
// touchpad settings do nothing, but the shell works, so it only warns.
func checkSource(path, target string) {
	where, overrides, err := bootstrap.FindSource(path, target)
	switch {
	case errors.Is(err, bootstrap.ErrNoSource):
		if strings.HasSuffix(path, ".lua") {
			fmt.Printf("%s does not load %s, so the keyboard and touchpad\n"+
				"settings in inua's settings window do nothing. Add this as its last line:\n\n    %s\n",
				path, hyprconf.LuaName, hyprconf.LuaRequire)
		} else {
			fmt.Printf("%s does not source %s, so the keyboard and touchpad\n"+
				"settings in inua's settings window do nothing. Add this as its last line:\n\n    source = ./%s\n",
				path, hyprconf.Name, hyprconf.Name)
		}
	case err != nil:
		fmt.Printf("%s: %v\n", path, err)
	default:
		fmt.Printf("%s: loads %s (%s)\n", path, filepath.Base(target), where)
		for _, o := range overrides {
			fmt.Printf("%s: set after %s, so it overrides inua's setting; move the line that loads it below\n", o, filepath.Base(target))
		}
	}
}

// applyHyprland writes hypr/inua.conf (inua.lua for hyprland.lua) from the settings file and has
// Hyprland reload it, as the settings window does when it saves.
func applyHyprland(path string) error {
	c, warnings, err := config.Load(path)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Printf("%s: %s\n", path, w)
	}
	var client *hypr.Client
	if paths, err := hypr.PathsFromEnv(); err == nil {
		client = hypr.NewClient(paths)
	}
	target := hyprconf.Target(hyprConfig(path))
	changed, err := hyprconf.Apply(context.Background(), c, target, client)
	switch {
	case err != nil:
		return err
	case !changed:
		fmt.Printf("%s: up to date\n", target)
	case client == nil:
		fmt.Printf("%s: written; Hyprland is not running and reads it when it starts\n", target)
	default:
		fmt.Printf("%s: written; Hyprland reloaded\n", target)
	}
	return nil
}

// checkConfig reports problems in a settings file the way inua-shell
// reads it.
func checkConfig(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("%s: not found; inua-shell uses the defaults\n", path)
		return nil
	}
	_, warnings, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%w (inua-shell would use the defaults)", err)
	}
	for _, w := range warnings {
		fmt.Printf("%s: %s\n", path, w)
	}
	if len(warnings) > 0 {
		return fmt.Errorf("%d problem(s); inua-shell uses the defaults for those settings", len(warnings))
	}
	fmt.Printf("%s: ok\n", path)
	return nil
}

// cmdInit writes the sample configuration (config/ in the repository,
// embedded here) under the config directory, then checks it.
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "replace files that differ from the samples, keeping the old ones as .bak")
	pinentry := fs.String("pinentry", "ask", "when gpg-agent uses another pinentry: ask, replace it with inua-pinentry, or keep it")
	dir := fs.String("dir", filepath.Dir(filepath.Dir(config.DefaultPath())), "config directory to write into")
	fs.Parse(args)
	if fs.NArg() != 0 || (*pinentry != "ask" && *pinentry != "replace" && *pinentry != "keep") {
		usage()
	}
	results, err := bootstrap.Install(samples.FS, samples.Files, *dir, *force)
	for _, line := range bootstrap.Lines(results, filepath.Join(*dir, filepath.FromSlash(samples.IconSet))) {
		fmt.Println(line)
	}
	if err != nil {
		return err
	}
	if *dir == filepath.Dir(filepath.Dir(config.DefaultPath())) { // the user's own setup
		if err := initPinentry(*pinentry, *force); err != nil {
			fmt.Println("gpg:", err)
		}
	}

	fmt.Println()
	failed := false
	if err := checkConfig(filepath.Join(*dir, "inua", "config.toml")); err != nil {
		fmt.Println("inua:", err)
		failed = true
	}
	hyprConf := hyprconf.ConfigFile(filepath.Join(*dir, "hypr"), false)
	if bootstrap.OmarchyShell(hyprConf) {
		initOmarchy(hyprConf, *force)
	}
	if target := hyprconf.Target(hyprConf); strings.HasSuffix(target, ".lua") {
		// A Lua config gets inua.lua now, for the line that loads it.
		cfg, _, _ := config.Load(filepath.Join(*dir, "inua", "config.toml"))
		if changed, err := hyprconf.Apply(context.Background(), cfg, target, nil); err != nil {
			fmt.Printf("%s: %v\n", target, err)
		} else if changed {
			fmt.Printf("%s: written\n", target)
		}
	}
	if _, err := exec.LookPath("Hyprland"); err != nil {
		fmt.Println("Hyprland is not installed (make deps); hyprland.conf not checked")
	} else {
		out, _ := exec.Command("Hyprland", "--verify-config", "-c", hyprConf).CombinedOutput()
		if strings.Contains(string(out), "config ok") {
			fmt.Printf("%s: ok\n", hyprConf)
		} else {
			fmt.Printf("%s: Hyprland reports problems:\n%s\n", hyprConf, strings.TrimSpace(lastLines(string(out), 6)))
			failed = true
		}
	}
	if err := checkHyprland(hyprConf); err != nil {
		failed = true
	}
	if _, err := exec.LookPath("inua-shell"); err != nil {
		fmt.Println("inua-shell is not on PATH; hyprland.conf starts it by name (make install)")
	}
	if failed {
		return fmt.Errorf("the configuration has problems (see above)")
	}
	fmt.Println("\nLog out and choose the Inua session; Super+Space opens the launcher.")
	return nil
}

// initOmarchy has Omarchy's config at path start inua-shell in place of
// Omarchy's shell (Quickshell), which would run beside it, when the user
// says so (the default answer yes) or with -force. Both the Omarchy and
// the Inua session read the file.
func initOmarchy(path string, force bool) {
	if !force && !askYes("Omarchy starts its own shell (Quickshell), which would run beside inua's. Start inua-shell in its place, in Omarchy's session too?") {
		fmt.Printf("%s: Omarchy's shell left as it is\n", path)
		return
	}
	if err := bootstrap.ReplaceOmarchyShell(path); err != nil {
		fmt.Printf("%s: %v\n", path, err)
		return
	}
	fmt.Printf("%s: starts inua-shell in place of Omarchy's shell (the old file is %s.bak)\n", path, filepath.Base(path))
}

// initPinentry has gpg-agent ask for passphrases in inua's dialog, and
// reloads it if that changed. Another pinentry already set is replaced
// when the user says so (mode "ask", the default answer yes), "replace"
// or -force; "keep" leaves gpg-agent as it is.
func initPinentry(mode string, force bool) error {
	if mode == "keep" {
		fmt.Println("gpg-agent left as it is (-pinentry keep)")
		return nil
	}
	prog, err := exec.LookPath("inua-pinentry")
	if err != nil {
		fmt.Println("inua-pinentry is not on PATH (make install); gpg-agent left as it is")
		return nil
	}
	if prog, err = filepath.Abs(prog); err != nil {
		return err
	}
	replace := force || mode == "replace"
	if cur := bootstrap.Pinentry(bootstrap.GnuPGHome()); !replace && cur != prog {
		replace = askYes(fmt.Sprintf("gpg asks for passphrases with %s. Ask in inua's dialog instead?", cmp.Or(cur, "the system's pinentry")))
		if !replace {
			fmt.Println("gpg-agent left as it is")
			return nil
		}
	}
	changed, msg, err := bootstrap.SetPinentry(bootstrap.GnuPGHome(), prog, replace)
	if err != nil {
		return err
	}
	fmt.Println(msg)
	if changed {
		if _, err := exec.LookPath("gpgconf"); err == nil {
			if out, err := exec.Command("gpgconf", "--reload", "gpg-agent").CombinedOutput(); err != nil {
				return fmt.Errorf("reloading gpg-agent: %v %s", err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

// askYes asks a yes-or-no question on the terminal, yes by default. It
// reads /dev/tty, as the installer runs inua down a pipe; without a
// terminal the default stands.
func askYes(question string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return true
	}
	defer tty.Close()
	fmt.Fprintf(tty, "%s [Y/n] ", question)
	answer, _ := bufio.NewReader(tty).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return !strings.HasPrefix(answer, "n")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
