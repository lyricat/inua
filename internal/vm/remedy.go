package vm

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// Use returns the login name a remedy runs for: the placeholders in a
// remedy's command are filled with it.
func currentUser() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		// Inside a service the passwd entry can be the wrong one; the
		// environment is what the session was started with.
		if fromEnv := os.Getenv("USER"); fromEnv != "" {
			return fromEnv, nil
		}
		return u.Username, nil
	}
	if name := os.Getenv("USER"); name != "" {
		return name, nil
	}
	return "", errors.New("cannot tell which user this is")
}

// ApplyRemedy carries out a check's remedy. A privileged remedy goes through
// pkexec, so the session's polkit agent asks for the password; the command
// itself never runs with the caller's own rights.
//
// It returns what was done and whether a fresh login is needed: changing a
// process's group list cannot be done from inside that process, so a group
// change always needs one.
func ApplyRemedy(r Remedy) (done string, relogin bool, err error) {
	if len(r.Argv) == 0 {
		return "", false, errors.New("this check has nothing Inua can fix")
	}
	name, err := currentUser()
	if err != nil {
		return "", false, err
	}
	argv := make([]string, len(r.Argv))
	for i, arg := range r.Argv {
		arg = strings.ReplaceAll(arg, "{User}", name)
		if r.Group != "" {
			arg = strings.ReplaceAll(arg, "{Group}", r.Group)
		}
		argv[i] = arg
	}
	if r.Privileged {
		// pkexec runs the program directly; no shell, so nothing in the
		// arguments can be taken as one.
		argv = append([]string{"pkexec"}, argv...)
	}
	prog, err := exec.LookPath(argv[0])
	if err != nil {
		return "", false, fmt.Errorf("%s is not installed", argv[0])
	}
	cmd := exec.Command(prog, argv[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// pkexec says why in its own exit codes; keep the message short and
		// leave the password and the command line out of it.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 126 {
			return "", false, errors.New("the request was not authorised")
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", false, errors.New(msg)
	}
	return r.What, r.Relogin, nil
}
