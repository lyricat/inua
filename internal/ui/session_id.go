package ui

import (
	"os"
	"strings"
)

// loginSessionID is the logind session the shell runs in. Display managers
// do not always put XDG_SESSION_ID in the graphical session's environment, so
// fall back to the session scope in the process's cgroup.
func loginSessionID() string {
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		return id
	}
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		_, rest, ok := strings.Cut(line, "session-")
		if !ok {
			continue
		}
		id, _, _ := strings.Cut(rest, ".")
		if id != "" && validSessionID(id) {
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
