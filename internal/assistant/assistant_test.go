package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/integration"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desk"
)

// fakeModel speaks the OpenAI chat API, replying from a script: each reply
// either calls tools or is the agent's final JSON. It records what it was
// sent.
type fakeModel struct {
	t       *testing.T
	mu      sync.Mutex
	replies []reply
	got     []map[string]any
}

type reply struct {
	tool, args string // a tool call
	final      string // or a final answer
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.got = append(f.got, req)
	if len(f.replies) == 0 {
		f.mu.Unlock()
		http.Error(w, "no more replies", http.StatusInternalServerError)
		return
	}
	rep := f.replies[0]
	f.replies = f.replies[1:]
	n := len(f.got)
	f.mu.Unlock()

	content := ""
	var calls []map[string]any
	if rep.tool != "" {
		calls = []map[string]any{{"index": 0, "id": fmt.Sprintf("call_%d", n), "type": "function",
			"function": map[string]any{"name": rep.tool, "arguments": rep.args}}}
	} else {
		b, _ := json.Marshal(map[string]any{"type": "final", "output": rep.final})
		content = string(b)
	}
	finish := "stop"
	if calls != nil {
		finish = "tool_calls"
	}
	if stream, _ := req["stream"].(bool); stream {
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
		chunk := func(delta map[string]any, finish any) map[string]any {
			return map[string]any{"id": "x", "object": "chat.completion.chunk", "created": 1, "model": "fake",
				"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}}
		}
		send(chunk(map[string]any{"role": "assistant"}, nil))
		if calls != nil {
			send(chunk(map[string]any{"tool_calls": calls}, nil))
		}
		for i := 0; i < len(content); i += 7 { // in pieces, as a model streams
			send(chunk(map[string]any{"content": content[i:min(i+7, len(content))]}, nil))
		}
		send(chunk(map[string]any{}, finish))
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	msg := map[string]any{"role": "assistant", "content": content}
	if calls != nil {
		msg["tool_calls"] = calls
	}
	json.NewEncoder(w).Encode(map[string]any{"id": "x", "object": "chat.completion", "created": 1, "model": "fake",
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
}

// newModelServer serves the fake model; its URL.
func newModelServer(t *testing.T, model *fakeModel) string {
	t.Helper()
	srv := httptest.NewServer(model)
	t.Cleanup(srv.Close)
	return srv.URL
}

func newTest(t *testing.T, model *fakeModel, own []desk.Tool, approve Approver) *Assistant {
	t.Helper()
	srv := newModelServer(t, model)
	a, err := New(Settings{Profiles: []Profile{{Name: "test", Provider: "openai_chat_compatible", Model: "gpt-4.1-mini", Endpoint: srv + "/v1", APIKey: "test"}},
		AskBeforeChanges: true, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 6}, own, approve)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAsk(t *testing.T) {
	focus := false
	setFocus := desk.Tool{
		Name: "set_focus", Description: "Turns Focus (quiet notifications) on or off.",
		Schema: `{"type":"object","properties":{"on":{"type":"boolean"}},"required":["on"]}`,
		Risk:   desk.Change,
		Describe: func(args map[string]any) string {
			if on, _ := args["on"].(bool); on {
				return "Turning on Focus"
			}
			return "Turning off Focus"
		},
		Run: func(_ context.Context, args map[string]any) (any, error) {
			focus, _ = args["on"].(bool)
			return map[string]any{"focus": focus}, nil
		},
	}
	model := &fakeModel{t: t, replies: []reply{
		{tool: "set_focus", args: `{"on":true}`},
		{final: "Focus is on: notifications stay quiet."},
		{final: "Yes, it is still on."},
	}}
	var asked []string
	a := newTest(t, model, []desk.Tool{setFocus}, func(_ context.Context, tool, doing string) (bool, error) {
		asked = append(asked, doing)
		return true, nil
	})

	var mu sync.Mutex
	var lines, answers []string
	ev := Events{
		Tool:   func(name, doing string) { mu.Lock(); lines = append(lines, doing); mu.Unlock() },
		Answer: func(text string) { mu.Lock(); answers = append(answers, text); mu.Unlock() },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	answer, err := a.Ask(ctx, "Turn on Focus", ev)
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Focus is on: notifications stay quiet." || !focus {
		t.Errorf("answer %q, focus %v", answer, focus)
	}
	if len(asked) != 1 || asked[0] != "Turning on Focus" || len(lines) != 1 {
		t.Errorf("asked %q, lines %q", asked, lines)
	}
	if len(answers) == 0 || answers[len(answers)-1] != answer {
		t.Errorf("streamed %q", answers)
	}
	for _, a := range answers {
		if strings.Contains(a, `"type"`) {
			t.Errorf("the window saw JSON: %q", a)
		}
	}

	// The second question carries the first: the model is sent both.
	if _, err := a.Ask(ctx, "Is it still on?", Events{}); err != nil {
		t.Fatal(err)
	}
	last, _ := json.Marshal(model.got[len(model.got)-1]["messages"])
	if !strings.Contains(string(last), "Turn on Focus") || !strings.Contains(string(last), "Focus is on") {
		t.Errorf("the conversation was not carried: %s", last)
	}
	// inua's tool and policy reached the model.
	first, _ := json.Marshal(model.got[0])
	if !strings.Contains(string(first), "set_focus") || !strings.Contains(string(first), "assistant of inua") {
		t.Errorf("the first request lacks inua's tool or prompt")
	}
}

func TestDeclined(t *testing.T) {
	ran := false
	power := desk.Tool{Name: "power", Description: "Shuts down.", Risk: desk.Always,
		Run: func(context.Context, map[string]any) (any, error) { ran = true; return "ok", nil }}
	model := &fakeModel{t: t, replies: []reply{
		{tool: "power", args: `{}`},
		{final: "I did not shut down: you declined."},
	}}
	var denied []string
	a := newTest(t, model, []desk.Tool{power}, func(context.Context, string, string) (bool, error) { return false, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	answer, err := a.Ask(ctx, "Shut down", Events{Denied: func(name, _ string) { denied = append(denied, name) }})
	if err != nil {
		t.Fatal(err)
	}
	if ran || len(denied) != 1 || !strings.Contains(answer, "declined") {
		t.Errorf("ran %v, denied %q, answer %q", ran, denied, answer)
	}
	// The model was told it was declined, not that it failed.
	if last, _ := json.Marshal(model.got[len(model.got)-1]); !strings.Contains(string(last), "declined") {
		t.Error("the model was not told")
	}
}

func TestSettings(t *testing.T) {
	c := config.Default().Assistant
	c.Profiles = []config.AIProfile{
		{Name: "main", Provider: "openai", Model: "gpt-5.2"},
		{Name: "cheap", Provider: "openai", Model: "gpt-5-mini"},
		{Name: "claude", Provider: "anthropic", Model: "claude-sonnet-5-5"},
		{Name: "local", Provider: "openai_chat_compatible", Model: "qwen3", Endpoint: "http://127.0.0.1:11434/v1"},
	}
	c.Profile, c.Fallback = "main", []string{"claude", "local"}
	if _, err := SettingsFrom(c, nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key for the answering profile: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-env") // keys come only from the keyring
	s, err := SettingsFrom(c, map[string]string{"main": "sk-saved", "cheap": "sk-cheap"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range s.Profiles {
		names = append(names, p.Name)
	}
	// claude has none and is left out, also as a
	// fallback; a local server needs none.
	if s.Profiles[0].APIKey != "sk-saved" || s.Profiles[1].APIKey != "sk-cheap" || fmt.Sprint(names) != "[main cheap local]" ||
		fmt.Sprint(s.Missing) != "[claude]" || fmt.Sprint(s.Fallback) != "[local]" {
		t.Errorf("%+v", s)
	}
}

// Abilities: the window switches only to profiles that answer, quick
// choices go only to profiles that make them, the one for them first.
func TestAbilities(t *testing.T) {
	c := config.Default().Assistant
	c.Profiles = []config.AIProfile{
		{Name: "main", Provider: "openai", Model: "gpt-5.2", Abilities: []string{"text"}},
		{Name: "both", Provider: "openai", Model: "gpt-5-mini"},
		{Name: "judge", Provider: "openai", Model: "gpt-6-luna", Abilities: []string{"decision"}},
		{Name: "nokey", Provider: "openai", Model: "gpt-5-mini", Abilities: []string{"decision"}},
	}
	c.Profile, c.Fallback = "main", []string{"judge", "both"}
	keys := map[string]string{"main": "k", "judge": "k", "both": "k"}
	for _, tc := range []struct{ decision, want string }{
		{"", "both"},       // main makes none: the first that does
		{"judge", "judge"}, // as asked
		{"nokey", "both"},  // without a key: the first that does
	} {
		c.Decision = tc.decision
		s, err := SettingsFrom(c, keys)
		if err != nil {
			t.Fatal(err)
		}
		if s.Decision != tc.want || fmt.Sprint(s.Fallback) != "[both]" {
			t.Errorf("decision %q: %+v", tc.decision, s)
		}
	}
	c.Decision = "judge"
	s, _ := SettingsFrom(c, keys)
	s.StateDir, s.CacheDir = t.TempDir(), t.TempDir()
	a, err := New(s, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(a.Profiles()) != "[main both]" || fmt.Sprint(a.Choosers()) != "[judge both]" {
		t.Errorf("profiles %q, choosers %q", a.Profiles(), a.Choosers())
	}
	if a.UseProfile("judge") == nil {
		t.Error("switched to a profile that only chooses")
	}
	if _, err := a.Choose(context.Background(), "main", "Pick.", "", []string{"a", "b"}); err == nil || errors.Is(err, ErrNoChoice) {
		t.Errorf("main chose: %v", err)
	}
	c.Profile = "judge"
	if _, err := SettingsFrom(c, keys); err == nil {
		t.Error("judge answers")
	}
}

func TestProfiles(t *testing.T) {
	var models []string
	var mu sync.Mutex
	model := &fakeModel{t: t, replies: []reply{{final: "from backup"}, {final: "from other"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		mu.Lock()
		models = append(models, fmt.Sprint(req["model"]))
		mu.Unlock()
		if req["model"] == "broken" { // the answering profile fails: the fallback answers
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		model.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	p := func(name, model string) Profile {
		return Profile{Name: name, Provider: "openai_chat_compatible", Model: model, Endpoint: srv.URL + "/v1", APIKey: "k"}
	}
	a, err := New(Settings{Profiles: []Profile{p("main", "broken"), p("backup", "backup-model"), p("other", "other-model")},
		Fallback: []string{"backup"}, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 4}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if answer, err := a.Ask(ctx, "hi", Events{}); err != nil || answer != "from backup" {
		t.Errorf("fallback: %q %v (asked %q)", answer, err, models)
	}
	if err := a.UseProfile("other"); err != nil {
		t.Fatal(err)
	}
	if answer, err := a.Ask(ctx, "hi", Events{}); err != nil || answer != "from other" || a.Model() != "other-model · openai_chat_compatible" {
		t.Errorf("switched: %q %v %q", answer, err, a.Model())
	}
	if a.UseProfile("nope") == nil {
		t.Error("no such profile")
	}
}

// An assistant with tools may plan: the model is offered plan_create with
// them, and not without them.
func TestPlanTool(t *testing.T) {
	for _, tools := range [][]string{{"url_fetch"}, nil} {
		model := &fakeModel{t: t, replies: []reply{{final: "hi"}}}
		srv := newModelServer(t, model)
		a, err := New(Settings{Profiles: []Profile{{Name: "test", Provider: "openai_chat_compatible", Model: "gpt-4.1-mini", Endpoint: srv + "/v1", APIKey: "test"}},
			Tools: tools, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 2}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Ask(context.Background(), "hello", Events{}); err != nil {
			t.Fatal(err)
		}
		model.mu.Lock()
		sent, _ := json.Marshal(model.got)
		model.mu.Unlock()
		if has := bytes.Contains(sent, []byte(`"plan_create"`)); has != (tools != nil) {
			t.Errorf("tools %v: plan_create offered %v", tools, has)
		}
	}
}

// With elevated commands on, the bash tool offers to run a command as root
// (after the dialog); off, or without bash, it does not.
func TestElevatedCommands(t *testing.T) {
	// A desktop session with sudo and a dialog, whatever machine runs it.
	bin := t.TempDir()
	for _, name := range []string{"sudo", "zenity"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	integration.RunAskpassIfRequested() // as inua-assistant's main does
	for _, c := range []struct {
		elevated bool
		tools    []string
		want     bool
	}{{true, []string{"bash"}, true}, {false, []string{"bash"}, false}, {true, []string{"url_fetch"}, false}} {
		model := &fakeModel{t: t, replies: []reply{{final: "hi"}}}
		srv := newModelServer(t, model)
		a, err := New(Settings{Profiles: []Profile{{Name: "test", Provider: "openai_chat_compatible", Model: "gpt-4.1-mini", Endpoint: srv + "/v1", APIKey: "test"}},
			Tools: c.tools, Elevated: c.elevated, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 2}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Ask(context.Background(), "hello", Events{}); err != nil {
			t.Fatal(err)
		}
		model.mu.Lock()
		sent, _ := json.Marshal(model.got)
		model.mu.Unlock()
		if has := bytes.Contains(sent, []byte(`"elevated"`)); has != c.want {
			t.Errorf("elevated %v, tools %v: offered %v", c.elevated, c.tools, has)
		}
		if told := bytes.Contains(sent, []byte("administrator rights ]]")); told != c.want {
			t.Errorf("elevated %v, tools %v: told %v", c.elevated, c.tools, told)
		}
	}
}
