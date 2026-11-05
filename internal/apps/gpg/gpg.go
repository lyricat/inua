// Package gpg encrypts and decrypts text and files with GnuPG for inua's
// GPG app, through gpg itself: the user's keyring, gpg-agent and its
// pinentry (inua's passphrase dialog). It encrypts to keys, or with a
// passphrase alone; it lists the keyring, and reads gpg's status lines
// for who a message was encrypted to and who signed it
// (docs/feat/gpg.md).
package gpg

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// runTime is how long gpg may take; signing and decrypting wait for the
// passphrase, which the user may take a while to type.
const runTime = 10 * time.Minute

// Key is a key in the keyring.
type Key struct {
	Fingerprint string
	KeyID       string // the last 16 hex digits
	UIDs        []UID  // the valid ones first
	Algo        string // "ed25519", "RSA 4096"
	Created     time.Time
	Expires     time.Time // zero when it does not
	Validity    string    // gpg's: u ultimate, f full, m marginal, n never, "" unknown
	Revoked     bool
	Expired     bool
	Disabled    bool
	CanEncrypt  bool // the key or one of its subkeys
	CanSign     bool
	Secret      bool // its secret key is here: it is the user's
	// SubkeyIDs are its subkeys' key IDs: a message names the encryption
	// subkey it was encrypted to.
	SubkeyIDs []string
}

// UID is a user ID: "Name (Comment) <email>".
type UID struct{ Name, Email, Comment string }

// String writes it as gpg does.
func (u UID) String() string {
	s := u.Name
	if u.Comment != "" {
		s += " (" + u.Comment + ")"
	}
	if u.Email != "" {
		s = strings.TrimSpace(s + " <" + u.Email + ">")
	}
	return s
}

// Name is the key's first user ID, as a person reads it.
func (k Key) Name() string {
	if len(k.UIDs) == 0 {
		return k.KeyID
	}
	return cmp.Or(k.UIDs[0].Name, k.UIDs[0].Email)
}

// Usable reports whether the key can be used at all: not revoked,
// expired or disabled.
func (k Key) Usable() bool { return !k.Revoked && !k.Expired && !k.Disabled }

// Keys lists the keyring: the user's keys (with a secret key) first,
// then by name.
func Keys() ([]Key, error) {
	pub, err := run(nil, "--with-colons", "--fixed-list-mode", "--list-keys")
	if err != nil {
		return nil, err
	}
	sec, _ := run(nil, "--with-colons", "--fixed-list-mode", "--list-secret-keys")
	keys := ParseKeys(pub)
	secret := map[string]bool{}
	for _, k := range ParseKeys(sec) {
		secret[k.Fingerprint] = true
	}
	for i := range keys {
		keys[i].Secret = secret[keys[i].Fingerprint]
	}
	slices.SortStableFunc(keys, func(a, b Key) int {
		if a.Secret != b.Secret {
			if a.Secret {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
	return keys, nil
}

// ParseKeys reads gpg's --with-colons --fixed-list-mode listing of keys
// (public or secret).
func ParseKeys(listing []byte) []Key {
	var keys []Key
	var cur *Key
	inPrimary := false
	stale := map[int][]UID{}
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
		case "pub", "sec":
			keys = append(keys, Key{
				Secret:     field(0) == "sec",
				KeyID:      field(4),
				Algo:       algo(field(3), field(2), field(16)),
				Created:    unix(field(5)),
				Expires:    unix(field(6)),
				Validity:   validity(field(1)),
				Revoked:    field(1) == "r",
				Expired:    field(1) == "e",
				Disabled:   strings.Contains(field(11), "D"),
				CanEncrypt: strings.Contains(field(11), "E"),
				CanSign:    strings.Contains(field(11), "S"),
			})
			cur, inPrimary = &keys[len(keys)-1], true
		case "sub", "ssb":
			inPrimary = false
			if cur != nil && field(4) != "" {
				cur.SubkeyIDs = append(cur.SubkeyIDs, field(4))
			}
		case "fpr":
			if cur != nil && inPrimary && cur.Fingerprint == "" {
				cur.Fingerprint = field(9)
			}
		case "uid":
			if cur == nil {
				continue
			}
			u := splitUID(unescape(field(9)))
			if field(1) == "r" || field(1) == "e" {
				// A revoked or expired user ID only names a key that has
				// no other, as an expired key's are.
				stale[len(keys)-1] = append(stale[len(keys)-1], u)
				continue
			}
			cur.UIDs = append(cur.UIDs, u)
		}
	}
	for i, uids := range stale {
		if len(keys[i].UIDs) == 0 {
			keys[i].UIDs = uids
		}
	}
	return keys
}

func validity(v string) string {
	switch v {
	case "u", "f", "m", "n":
		return v
	}
	return ""
}

func algo(id, bits, curve string) string {
	if curve != "" {
		return curve
	}
	name := map[string]string{"1": "RSA", "2": "RSA", "3": "RSA", "16": "ElGamal", "17": "DSA", "18": "ECDH", "19": "ECDSA", "22": "EdDSA"}[id]
	if name != "" && bits != "" {
		return name + " " + bits
	}
	return name
}

func unix(s string) time.Time {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0)
	}
	return time.Time{}
}

// unescape undoes gpg's \xNN escapes in a --with-colons field.
func unescape(s string) string {
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

// splitUID splits "Name (Comment) <email>"; a user ID that is only an
// address is the email.
func splitUID(uid string) UID {
	uid = strings.TrimSpace(uid)
	var u UID
	if i := strings.LastIndexByte(uid, '<'); i >= 0 && strings.HasSuffix(uid, ">") {
		u.Email, uid = uid[i+1:len(uid)-1], strings.TrimSpace(uid[:i])
	} else if strings.Contains(uid, "@") && !strings.Contains(uid, " ") {
		u.Email = uid
		return u
	}
	if i := strings.IndexByte(uid, '('); i >= 0 && strings.HasSuffix(uid, ")") {
		u.Comment, uid = uid[i+1:len(uid)-1], strings.TrimSpace(uid[:i])
	}
	u.Name = uid
	return u
}

// Export is a key's public key, armored.
func Export(fingerprint string) (string, error) {
	out, err := run(nil, "--armor", "--export", "--", fingerprint)
	if err == nil && len(out) == 0 {
		err = fmt.Errorf("no public key %s", fingerprint)
	}
	return string(out), err
}

// Armored finds the first whole ASCII-armored block in a text: its kind
// ("MESSAGE", "SIGNED MESSAGE", "PUBLIC KEY BLOCK", "PRIVATE KEY BLOCK",
// "SIGNATURE") and the block itself.
func Armored(text string) (kind, block string, ok bool) {
	const begin = "-----BEGIN PGP "
	i := strings.Index(text, begin)
	if i < 0 {
		return "", "", false
	}
	head, _, found := strings.Cut(text[i+len(begin):], "-----")
	if !found || head == "" || strings.ContainsAny(head, "\r\n") {
		return "", "", false
	}
	end := "-----END PGP " + head + "-----"
	if head == "SIGNED MESSAGE" {
		end = "-----END PGP SIGNATURE-----"
	}
	j := strings.Index(text[i:], end)
	if j < 0 {
		return "", "", false
	}
	return head, text[i:i+j+len(end)] + "\n", true
}

// ShowKeys lists the keys in data (a key file or a pasted key) without
// importing them.
func ShowKeys(data []byte) ([]Key, error) {
	out, err := run(data, "--with-colons", "--fixed-list-mode", "--show-keys")
	if err != nil {
		return nil, err
	}
	keys := ParseKeys(out)
	if len(keys) == 0 {
		return nil, errors.New("no key in it")
	}
	return keys, nil
}

// Import adds the keys in data to the keyring; it says the fingerprints
// of the keys added or changed.
func Import(data []byte) ([]string, error) {
	_, status, err := runStatus(data, "", "--import")
	var fprs []string
	for line := range strings.SplitSeq(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "[GNUPG:] IMPORT_OK "); ok {
			if f := strings.Fields(rest); len(f) > 1 && !slices.Contains(fprs, f[1]) {
				fprs = append(fprs, f[1])
			}
		}
	}
	if err != nil && len(fprs) > 0 {
		err = nil // some keys came in; gpg complains of the rest
	}
	return fprs, err
}

// Expiries are the lifetimes a new or extended key can be given, as gpg
// takes them.
var Expiries = []string{"1y", "2y", "5y", "never"}

// Generate makes the user a key, for name and email, expiring after
// expire (one of Expiries); gpg asks for its passphrase. It says the new
// key's fingerprint.
func Generate(name, email, expire string) (string, error) {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" && email == "" {
		return "", errors.New("no name or address")
	}
	if !slices.Contains(Expiries, expire) {
		return "", fmt.Errorf("unknown expiry %q", expire)
	}
	uid := UID{Name: name, Email: email}.String()
	// --yes: another key for the same name is allowed; the user may keep
	// one per purpose.
	_, status, err := runStatus(nil, "", "--yes", "--quick-generate-key", uid, "default", "default", expire)
	for line := range strings.SplitSeq(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "[GNUPG:] KEY_CREATED "); ok {
			if f := strings.Fields(rest); len(f) > 1 {
				return f[1], nil
			}
		}
	}
	if err == nil {
		err = errors.New("gpg made no key")
	}
	return "", err
}

// Extend sets when the user's key and its subkeys expire, after expire
// (one of Expiries) from now; gpg asks for the passphrase.
func Extend(fingerprint, expire string) error {
	if !slices.Contains(Expiries, expire) {
		return fmt.Errorf("unknown expiry %q", expire)
	}
	if _, err := run(nil, "--quick-set-expire", fingerprint, expire); err != nil {
		return err
	}
	_, err := run(nil, "--quick-set-expire", fingerprint, expire, "*")
	return err
}

// Delete takes another person's public key off the keyring. It refuses
// a key whose secret key is here: that is the user's, and without it
// nothing encrypted to it can be read again.
func Delete(fingerprint string) error {
	if sec, _ := run(nil, "--with-colons", "--list-secret-keys", "--", fingerprint); len(sec) > 0 {
		return errors.New("it is your own key")
	}
	_, err := run(nil, "--yes", "--delete-keys", "--", fingerprint)
	return err
}

// Options are how to encrypt: to keys, or with a passphrase.
type Options struct {
	Recipients []string // fingerprints; the user chose them, so their validity is not asked
	Signer     string   // a fingerprint of the user's to sign with; "" not to sign
	Armor      bool     // ASCII armor, for text
	// Passphrase encrypts with it alone, to no key: whoever has it can
	// decrypt. Not with Recipients or Signer.
	Passphrase string
}

func (o Options) args() ([]string, error) {
	var args []string
	switch {
	case o.Passphrase != "":
		// gpg is given one passphrase: signing as well, it would try this
		// one on the user's key.
		if len(o.Recipients) > 0 || o.Signer != "" {
			return nil, errors.New("a passphrase and keys together")
		}
		if strings.ContainsAny(o.Passphrase, "\r\n") {
			return nil, errors.New("a passphrase is one line")
		}
		// gpg reads it from fd 4 (runStatus): asked by gpg-agent, it would
		// be typed twice for each file.
		args = []string{"--pinentry-mode", "loopback", "--passphrase-fd", "4", "--symmetric"}
	case len(o.Recipients) == 0:
		return nil, errors.New("no recipient")
	default:
		args = []string{"--trust-model", "always", "--encrypt"}
		if o.Signer != "" {
			args = append(args, "--sign", "--local-user", o.Signer)
		}
		for _, r := range o.Recipients {
			args = append(args, "--recipient", r)
		}
	}
	if o.Armor {
		args = append(args, "--armor")
	}
	return args, nil
}

// Encrypt encrypts data.
func Encrypt(data []byte, o Options) ([]byte, error) {
	args, err := o.args()
	if err != nil {
		return nil, err
	}
	out, _, err := runStatus(data, o.Passphrase, args...)
	return out, err
}

// EncryptFile encrypts a file to a file beside it, NAME.gpg (NAME.asc
// armored), which must not exist yet; it says the file written.
func EncryptFile(path string, o Options) (string, error) {
	args, err := o.args()
	if err != nil {
		return "", err
	}
	out := path + ".gpg"
	if o.Armor {
		out = path + ".asc"
	}
	if _, err := os.Stat(out); err == nil {
		return "", fmt.Errorf("%s is already there", filepath.Base(out))
	}
	_, _, err = runStatus(nil, o.Passphrase, append(args, "--output", out, "--", path)...)
	return out, err
}

// ErrPassphrase is a passphrase that does not open the message, and
// ErrCanceled the user closing gpg's dialog: gpg's own last word for
// both is "Bad session key".
var (
	ErrPassphrase = errors.New("wrong passphrase")
	ErrCanceled   = errors.New("cancelled")
)

// Result is what decrypting found out.
type Result struct {
	Encrypted bool     // it was encrypted, and decrypted (not only signed)
	To        []string // the key IDs it was encrypted to
	// Passphrase: it was encrypted with a passphrase, which opens it
	// without a key.
	Passphrase bool
	Signed     bool
	// Signature: "good", "bad", "unknown" (a signer whose key is not in
	// the keyring), "expired", "revoked", or "" unsigned.
	Signature string
	Signer    string // who, as the signature names them
	SignerKey string // the signer's key ID, or its fingerprint once checked
	SignedAt  time.Time
	FileName  string // the name the encrypted data carries

	badPassphrase, canceled bool // why it failed
}

// failure is err, the error of a gpg that did not decrypt, said as it
// is where gpg's last word misleads.
func (r Result) failure(err error) error {
	switch {
	case err == nil:
		return nil
	case r.canceled:
		return ErrCanceled
	case r.badPassphrase:
		return ErrPassphrase
	}
	return err
}

// ParseStatus reads gpg's status lines (--status-fd).
func ParseStatus(status []byte) Result {
	var r Result
	for line := range strings.SplitSeq(string(status), "\n") {
		rest, ok := strings.CutPrefix(line, "[GNUPG:] ")
		if !ok {
			continue
		}
		word, args, _ := strings.Cut(rest, " ")
		f := strings.Fields(args)
		arg := func(n int) string {
			if n < len(f) {
				return f[n]
			}
			return ""
		}
		switch word {
		case "DECRYPTION_OKAY":
			r.Encrypted = true
		case "ENC_TO":
			if !slices.Contains(r.To, arg(0)) {
				r.To = append(r.To, arg(0))
			}
		case "NEED_PASSPHRASE_SYM":
			r.Passphrase = true
		case "CANCELED_BY_USER":
			r.canceled = true
		case "ERROR": // "symkey_decrypt.maybe_error 11_BAD_PASSPHRASE"
			if strings.HasPrefix(arg(0), "symkey_decrypt") && strings.Contains(arg(1), "BAD_PASSPHRASE") {
				r.badPassphrase = true
			}
		case "GOODSIG", "BADSIG", "EXPSIG", "EXPKEYSIG", "REVKEYSIG":
			r.Signed = true
			r.SignerKey = arg(0)
			r.Signer = strings.TrimSpace(strings.TrimPrefix(args, arg(0)))
			r.Signature = map[string]string{"GOODSIG": "good", "BADSIG": "bad", "EXPSIG": "expired", "EXPKEYSIG": "expired", "REVKEYSIG": "revoked"}[word]
		case "ERRSIG":
			r.Signed, r.Signature = true, "unknown"
			r.SignerKey = arg(0)
			if t := unix(arg(4)); !t.IsZero() {
				r.SignedAt = t
			}
		case "VALIDSIG":
			r.SignerKey = arg(0)
			if t := unix(arg(2)); !t.IsZero() {
				r.SignedAt = t
			}
		case "PLAINTEXT":
			if name := arg(2); name != "" {
				r.FileName = unescapePercent(name)
			}
		}
	}
	return r
}

// unescapePercent undoes the %XX escapes of a status line's argument.
func unescapePercent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Decrypt decrypts (or only verifies) data. gpg asks for what opens it,
// a key's passphrase or the message's own, in its dialog.
func Decrypt(data []byte) ([]byte, Result, error) {
	out, status, err := runStatus(data, "", "--decrypt")
	r := ParseStatus(status)
	if err != nil && (r.Encrypted || r.Signed) && len(out) > 0 {
		err = nil // decrypted, though the signature could not be checked
	}
	return out, r, r.failure(err)
}

// DecryptedName is where a decrypted file goes: beside it, without
// .gpg, .asc or .pgp, else with .decrypted added.
func DecryptedName(path string) string {
	for _, ext := range []string{".gpg", ".asc", ".pgp"} {
		if base, ok := strings.CutSuffix(path, ext); ok && filepath.Base(base) != "" && !strings.HasSuffix(base, "/") {
			return base
		}
	}
	return path + ".decrypted"
}

// DecryptFile decrypts a file to DecryptedName(path), which must not
// exist yet; it says the file written.
func DecryptFile(path string) (string, Result, error) {
	out := DecryptedName(path)
	if _, err := os.Stat(out); err == nil {
		return "", Result{}, fmt.Errorf("%s is already there", filepath.Base(out))
	}
	_, status, err := runStatus(nil, "", "--output", out, "--decrypt", "--", path)
	r := ParseStatus(status)
	if err != nil && (r.Encrypted || r.Signed) {
		if st, e := os.Stat(out); e == nil && st.Size() > 0 {
			err = nil
		}
	}
	return out, r, r.failure(err)
}

// testArgs come before every command's own, for tests to give gpg an
// empty passphrase instead of asking.
var testArgs []string

// run runs gpg in batch mode; the error carries gpg's own words.
func run(stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTime)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gpg", slices.Concat([]string{"--batch", "--no-tty"}, testArgs, args)...)
	var out, errb bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), gpgError(errb.String(), err)
	}
	return out.Bytes(), nil
}

// runStatus runs gpg as run does, its status lines read from fd 3. A
// passphrase, if any, is for gpg to read from fd 4 (--passphrase-fd 4):
// so it is in no argument list, where every program could read it.
func runStatus(stdin []byte, passphrase string, args ...string) (out, status []byte, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTime)
	defer cancel()
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, "gpg", slices.Concat([]string{"--batch", "--no-tty", "--status-fd", "3"}, testArgs, args)...)
	var outb, errb, statb bytes.Buffer
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Stdout, cmd.Stderr = &outb, &errb
	cmd.ExtraFiles = []*os.File{pw}
	var passR, passW *os.File
	if passphrase != "" {
		if passR, passW, err = os.Pipe(); err != nil {
			pr.Close()
			pw.Close()
			return nil, nil, err
		}
		cmd.ExtraFiles = append(cmd.ExtraFiles, passR)
	}
	err = cmd.Start()
	pw.Close()
	if passR != nil {
		passR.Close()
		if err == nil {
			// gpg reads it as it starts; from one that ended before, the
			// write fails, and gpg's own error says why.
			io.WriteString(passW, passphrase+"\n")
		}
		passW.Close()
	}
	if err != nil {
		pr.Close()
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		io.Copy(&statb, pr)
		pr.Close()
		close(done)
	}()
	err = cmd.Wait()
	<-done
	if err != nil {
		err = gpgError(errb.String(), err)
	}
	return outb.Bytes(), statb.Bytes(), err
}

// gpgError is gpg's last word on what went wrong, "gpg: " cut.
func gpgError(stderr string, err error) error {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(strings.TrimPrefix(lines[i], "gpg: ")); l != "" {
			return errors.New(l)
		}
	}
	return err
}
