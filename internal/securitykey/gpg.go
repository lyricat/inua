package securitykey

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Key is a gpg key whose secret parts are on this computer, so it has a
// passphrase to keep.
type Key struct {
	Fingerprint string
	UserID      string   // the primary user ID: "Ada Lovelace <ada@example.org>"
	Grips       []string // the keygrips of its secret parts here
}

// SecretKeys lists the keys among ids (as a .gpg-id names them: an
// address, a key ID or a fingerprint) whose secret parts are here, not
// only on a card.
func SecretKeys(ctx context.Context, ids []string) ([]Key, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"--batch", "--no-tty", "--no-auto-check-trustdb",
		"--with-colons", "--with-keygrip", "--fixed-list-mode", "--list-secret-keys", "--"}, ids...)
	out, err := exec.CommandContext(ctx, "gpg", args...).Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("gpg: %w", err)
	}
	return parseSecretKeys(out), nil
}

// parseSecretKeys reads gpg's --with-colons listing of secret keys (with
// --with-keygrip and --fixed-list-mode). Field 15 says where a part's
// secret is: empty or "+" here, "#" a stub, else the serial number of the
// card it is on; only one here has a passphrase.
func parseSecretKeys(listing []byte) []Key {
	var (
		keys    []Key
		cur     *Key
		secret  bool // the part being read has its secret here
		wantFpr bool // the next fpr is the primary key's
	)
	sc := bufio.NewScanner(bytes.NewReader(listing))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		field := func(n int) string {
			if n < len(f) {
				return f[n]
			}
			return ""
		}
		switch field(0) {
		case "sec":
			keys = append(keys, Key{})
			cur = &keys[len(keys)-1]
			wantFpr = true
			fallthrough
		case "ssb":
			token := field(14)
			secret = cur != nil && (token == "" || token == "+") && field(1) != "r" && field(1) != "e"
		case "fpr":
			if wantFpr && cur != nil {
				cur.Fingerprint = field(9)
				wantFpr = false
			}
		case "grp":
			if cur != nil && secret && field(9) != "" {
				cur.Grips = append(cur.Grips, field(9))
			}
		case "uid":
			if cur != nil && cur.UserID == "" && field(1) != "r" && field(1) != "e" {
				cur.UserID = unescapeColons(field(9))
			}
		}
	}
	out := keys[:0]
	for _, k := range keys {
		if k.Fingerprint != "" && len(k.Grips) > 0 {
			out = append(out, k)
		}
	}
	return out
}

// unescapeColons undoes gpg's \xNN escapes in a --with-colons field.
func unescapeColons(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ErrPassphrase is a passphrase that does not open the key, and
// ErrNoPassphrase a key that has none to keep.
var (
	ErrPassphrase   = errors.New("the passphrase does not open the key")
	ErrNoPassphrase = errors.New("the key has no passphrase")
)

// agentSocket is gpg-agent's socket, the agent started if it was not.
func agentSocket(ctx context.Context) (string, error) {
	exec.CommandContext(ctx, "gpgconf", "--launch", "gpg-agent").Run()
	out, err := exec.CommandContext(ctx, "gpgconf", "--list-dirs", "agent-socket").Output()
	if err != nil {
		return "", fmt.Errorf("gpgconf: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Verify asks gpg-agent whether passphrase opens the key with keygrip
// grip. The agent checks the passphrase given, not one it remembers
// (PASSWD --verify), and asks for it here rather than in a pinentry
// (loopback); nothing about the key changes.
func Verify(ctx context.Context, grip, passphrase string) error {
	sock, err := agentSocket(ctx)
	if err != nil {
		return err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	} else {
		conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	r := bufio.NewReader(conn)
	send := func(line string) error {
		_, err := conn.Write([]byte(line + "\n"))
		return err
	}
	// reply reads to the end of a command's answer, giving the
	// passphrase once when the agent asks for it and cancelling a second
	// ask: the passphrase did not open the key.
	asked := 0
	reply := func() error {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return err
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "OK" || strings.HasPrefix(line, "OK "):
				return nil
			case strings.HasPrefix(line, "ERR "):
				if asked > 0 {
					return ErrPassphrase
				}
				return fmt.Errorf("gpg-agent: %s", strings.TrimPrefix(line, "ERR "))
			case strings.HasPrefix(line, "INQUIRE PASSPHRASE") && asked == 0:
				asked++
				if err := send("D " + assuanEscape(passphrase)); err != nil {
					return err
				}
				if err := send("END"); err != nil {
					return err
				}
			case strings.HasPrefix(line, "INQUIRE "):
				asked++
				if err := send("CAN"); err != nil {
					return err
				}
			}
		}
	}
	if err := reply(); err != nil { // the greeting
		return err
	}
	if err := send("OPTION pinentry-mode=loopback"); err != nil {
		return err
	}
	if err := reply(); err != nil {
		return err
	}
	if err := send("PASSWD --verify " + grip); err != nil {
		return err
	}
	if err := reply(); err != nil {
		return err
	}
	if asked == 0 {
		return ErrNoPassphrase
	}
	return nil
}

// assuanEscape writes %, CR and LF as Assuan's %XX for a data line.
func assuanEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '%', '\r', '\n':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
