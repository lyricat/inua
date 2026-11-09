package mail

import (
	"regexp"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// Detail is a line of a message's details: its name, and its people or
// its text.
type Detail struct {
	Name   string
	People []Address // From, To …: each a person
	Text   string
	Mono   bool // an identifier: the Message-ID, the list's
}

// Details are what a message's header says of it, as a mail client's
// "show details" lists them: who wrote, sent and is answered, who it
// went to, when, the server that mailed it and the domain that signed
// it, the provider's checks, its list, its Message-ID. Lines with
// nothing to say are left out.
func Details(m *Message) []Detail {
	var out []Detail
	people := func(name string, as []Address) {
		if len(as) > 0 {
			out = append(out, Detail{Name: name, People: as})
		}
	}
	text := func(name, s string, mono bool) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, Detail{Name: name, Text: s, Mono: mono})
		}
	}
	people(i18n.T("From"), []Address{m.From})
	if m.Sender.Addr != "" && !strings.EqualFold(m.Sender.Addr, m.From.Addr) {
		people(i18n.T("Sent by"), []Address{m.Sender})
	}
	if len(m.ReplyTo) != 1 || !strings.EqualFold(m.ReplyTo[0].Addr, m.From.Addr) {
		people(i18n.T("Reply to"), m.ReplyTo)
	}
	people(i18n.T("To"), m.To)
	people(i18n.T("Cc"), m.Cc)
	people(i18n.T("Bcc"), m.Bcc)
	if !m.Date.IsZero() {
		text(i18n.T("Date"), i18n.Time(m.Date.Local(), "Mon Jan 2, 2006, 15:04:05 -0700"), false)
	}
	text(i18n.T("Subject"), m.Subject, false)
	if m.Header.Len() > 0 {
		text(i18n.T("Mailed by"), Domain(strings.Trim(m.Header.Get("Return-Path"), "<> ")), false)
		text(i18n.T("Signed by"), signedBy(m), false)
	}
	if r := authResults(m.Authentication); len(r) > 0 {
		var checks []string
		for _, k := range []string{"spf", "dkim", "dmarc"} {
			if v := r[k]; v != "" {
				checks = append(checks, strings.ToUpper(k)+" "+v)
			}
		}
		text(i18n.T("Checks"), strings.Join(checks, " · "), false)
	}
	if m.Header.Len() > 0 {
		text(i18n.T("Mailing list"), m.Header.Get("List-Id"), true)
	}
	if m.MessageID != "" {
		text(i18n.T("Message-ID"), "<"+strings.Trim(m.MessageID, "<>")+">", true)
	}
	return out
}

var (
	dkimPass = regexp.MustCompile(`(?i)\bdkim\s*=\s*pass\b[^;]*?\bheader\.[di]\s*=\s*@?([a-z0-9.-]+)`)
	dkimTag  = regexp.MustCompile(`(?i)(?:^|;)\s*d\s*=\s*([a-z0-9.-]+)`)
)

// signedBy is the domain whose DKIM signature the provider found good,
// else the first signature's domain, unchecked.
func signedBy(m *Message) string {
	for _, h := range m.Header.Values("Authentication-Results") {
		if f := dkimPass.FindStringSubmatch(h); f != nil {
			return strings.ToLower(f[1])
		}
	}
	if f := dkimTag.FindStringSubmatch(m.Header.Get("DKIM-Signature")); f != nil {
		return strings.ToLower(f[1])
	}
	return ""
}
