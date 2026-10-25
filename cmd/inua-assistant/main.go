// Command inua-assistant is the assistant's window: an agent with inua's
// tools (docs/feat/assistant.md). `inua-assistant --ask "…"` asks a
// question; with the window already open, the question goes to it.
// `inua-assistant --mcp` is inua as an MCP server (docs/feat/mcp.md);
// `inua-assistant --answer` answers the games' questions on stdio;
// `inua-assistant --benchmark PROFILE` runs a profile's benchmarks.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/quailyquaily/mistermorph/integration"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/assistantui"
)

func main() {
	// First: sudo starts this program for the password of a command run
	// as administrator (Mister Morph's askpass). inua-askpass asks, in the
	// shell's dialog showing the command; without it, Mister Morph's own
	// dialog does, and exits.
	askpassInstead()
	integration.RunAskpassIfRequested()
	ask := flag.String("ask", "", "a question to ask")
	background := flag.Bool("background", false, "start the assistant without showing its window")
	serveToken := flag.Bool("serve-token", false, "print the Console token from the keyring, creating it if missing")
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory")
	mcpServer := flag.Bool("mcp", false, "be inua's MCP server on stdio, for an AI agent (inua mcp)")
	answerer := flag.Bool("answer", false, "answer questions on stdio, a JSON object a line, without tools (for the games)")
	bench := flag.String("benchmark", "", "run a profile's benchmarks, a JSON object a line (for Settings → AI)")
	transcribe := flag.Bool("transcribe", false, "print the words in the WAV recording on stdin, with the voice input profile (for dictation)")
	flag.Parse()
	if *transcribe {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := runTranscribe(ctx)
		stop()
		os.Exit(code)
	}
	if *bench != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := runBenchmark(ctx, *bench)
		stop()
		os.Exit(code)
	}
	if *answerer {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := runAnswer(ctx)
		stop()
		os.Exit(code)
	}
	if *mcpServer {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := runMCP(ctx)
		stop()
		os.Exit(code)
	}
	if *serveToken {
		token, err := keys.New().ServeToken(true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "inua-assistant:", err)
			os.Exit(1)
		}
		fmt.Println(token)
		return
	}
	if *ask != "" && activateRunning("ask", []dbus.Variant{dbus.MakeVariant(*ask)}) {
		return
	}
	if *background && *ask == "" && activateRunning("background", nil) {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	assistantui.Register(ctx, *background)
	var args []string
	if *ask != "" {
		args = []string{"--ask", *ask}
	}
	code := appui.Run("assistant", args, *uiDir)
	stop()
	os.Exit(code)
}

// activateRunning reaches the existing process without showing its window
// for a background start (including after the shell restarts).
func activateRunning(action string, params []dbus.Variant) bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	const name = appui.IDBase + ".Assistant"
	var has bool
	if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&has) != nil || !has {
		return false
	}
	obj := conn.Object(name, "/io/github/lyricat/Inua/Assistant")
	err = obj.Call("org.gtk.Actions.Activate", 0, action, params, map[string]dbus.Variant{}).Err
	if err != nil {
		fmt.Fprintln(os.Stderr, "inua-assistant:", err)
		return false
	}
	return true
}

// askpassInstead becomes inua-askpass, beside this program, when sudo
// started this one as Mister Morph's askpass helper (MISTERMORPH_ASKPASS),
// with its arguments and environment: the command and the deadline.
func askpassInstead() {
	if os.Getenv("MISTERMORPH_ASKPASS") != "1" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	p := filepath.Join(filepath.Dir(exe), "inua-askpass")
	if _, err := os.Stat(p); err == nil {
		err = syscall.Exec(p, append([]string{p}, os.Args[1:]...), os.Environ())
		fmt.Fprintln(os.Stderr, "inua-assistant: inua-askpass:", err)
	}
}
