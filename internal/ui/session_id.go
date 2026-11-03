package ui

import (
	"os"
	"path/filepath"
	"strings"
)

// loginSessionID is the logind session of the graphical compositor. Display
// managers do not always put XDG_SESSION_ID in the graphical session's
// environment, and a shell restarted from a terminal lives in that terminal's
// session, so prefer Wayfire's own session scope.
func loginSessionID() string {
	if id := os.Getenv("XDG_SESSION_ID"); id != "" && validSessionID(id) {
		return id
	}
	if id := compositorSessionID("wayfire"); id != "" {
		return id
	}
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	return sessionIDFromCgroup(string(data))
}

func compositorSessionID(command string) string {
	paths, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(data)) != command {
			continue
		}
		cgroup, err := os.ReadFile(filepath.Join(filepath.Dir(path), "cgroup"))
		if err != nil {
			continue
		}
		if id := sessionIDFromCgroup(string(cgroup)); id != "" {
			return id
		}
	}
	return ""
}

func sessionIDFromCgroup(cgroup string) string {
	for _, line := range strings.Split(cgroup, "\n") {
		_, rest, ok := strings.Cut(line, "session-")
		if !ok {
			continue
		}
		id, _, _ := strings.Cut(rest, ".")
		if validSessionID(id) {
			return id
		}
	}
	return ""
}

func validSessionID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
