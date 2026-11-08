package ui

import (
	"context"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/upgrade"
)

// aboutSettings is the About page: the version, whether a newer release
// is out (read when the page is first shown, and on Check for updates),
// Upgrade, the daily check's switch, and the latest release's notes.
type aboutSettings struct {
	version, state gtk.Label
	check, upgrade gtk.Button
	daily          gtk.Switch
	changelog      gtk.Box

	// The system manifest, a card to share (settings_card.go), read each
	// time the page is shown.
	fetch                         gtk.Box
	fetchCopy, cardImage, cardSav gtk.Button
	card                          *shell.Card
	cardCodes                     cardCodes
	cardView                      *gtk.Widget // the card, drawn smaller
	cardColors                    map[string]string
	cardNote                      *gtk.Label // what Copy or Save did, under the card
	fetching                      bool

	// What GitHub said, kept while the shell runs.
	latest    string
	releases  []upgrade.Release
	checked   time.Time
	checking  bool
	upgrading bool
	said      string // what an upgrade that left the shell running said
	err       error
}

func (s *shellUI) getAboutSettings(ui *gtkx.Builder) {
	a := &s.settings.about
	ui.Get("about_version", &a.version)
	ui.Get("about_state", &a.state)
	ui.Get("about_check", &a.check)
	ui.Get("about_upgrade", &a.upgrade)
	ui.Get("about_daily", &a.daily)
	ui.Get("about_changelog", &a.changelog)
	ui.Get("about_fetch", &a.fetch)
	ui.Get("about_fetch_copy", &a.fetchCopy)
	ui.Get("about_card_image", &a.cardImage)
	ui.Get("about_card_save", &a.cardSav)
	a.fetchCopy.ConnectClicked(&s.onSettingsButton)
	a.cardImage.ConnectClicked(&s.onSettingsButton)
	a.cardSav.ConnectClicked(&s.onSettingsButton)
	if a.cardView != nil {
		gtkx.ForgetCanvas(a.cardView) // the old window's card
	}
	a.card, a.cardView, a.cardNote = nil, nil, nil // a new window: the old one's widgets are gone
	a.check.ConnectClicked(&s.onSettingsButton)
	a.upgrade.ConnectClicked(&s.onSettingsButton)
	a.daily.ConnectStateSet(&s.onSettingsSwitch)
	s.onSettingsPage = func(uintptr, uintptr, uintptr) {
		s.selectSettingsIndex() // the page turned from Go: `inua settings PAGE`
		page := s.settings.pages.GetVisibleChildName()
		// A page left closes its panel, which gives the width back.
		if a := &s.settings.ai; page != "ai" && a.open >= 0 {
			a.open = -1
			s.showAIDetail()
		}
		if page != "keyring" && s.settings.keyring.open >= 0 {
			s.openKeyringRead(-1)
		}
		s.syncBluetoothSearch() // only while its page is shown
		s.pollWifi()            // likewise
		if page == "about" {
			s.fetchSystem()
		}
		if page == "power" {
			s.readPower() // hypridle's file and logind's settings, as they are now
		}
		if page == "securitykeys" {
			s.showSecurityKeysPage() // the keys plugged in now, and looking for more
		}
		switch {
		case page == "about" && s.settings.about.checked.IsZero():
			s.checkReleases()
		case page == "keyring":
			s.showKeyringReads() // what was read since it was last shown
		case page == "apps":
			s.showDefaultApps() // apps installed or chosen elsewhere since
			s.showPortals()     // inua portals run since, a backend started
		}
	}
	gtkx.ConnectNotify(&s.settings.pages, "visible-child-name", &s.onSettingsPage)
}

// aboutButton handles the page's buttons; false for another page's.
func (s *shellUI) aboutButton(name string) bool {
	a := &s.settings.about
	switch name {
	case "about_check":
		s.checkReleases()
	case "about_fetch_copy":
		s.copyCardText()
	case "about_card_image":
		s.copyCardImage()
	case "about_card_save":
		s.saveCard()
	case "about_upgrade":
		if a.latest == "" || a.upgrading {
			break
		}
		a.upgrading = true
		s.showAboutSettings()
		tag := a.latest
		go func() {
			said := s.runUpgrade(tag) // installed, the shell restarts on the new version
			gtkx.Post(func() {
				a.upgrading, a.said = false, said
				s.showAboutSettings()
			})
		}()
	default:
		return false
	}
	return true
}

// checkReleases asks GitHub for the latest release and the notes of the
// last few.
func (s *shellUI) checkReleases() {
	a := &s.settings.about
	if a.checking {
		return
	}
	a.checking, a.said = true, ""
	s.showAboutSettings()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		src := upgrade.GitHub()
		src.Client = &http.Client{}
		latest, err := src.Latest(ctx)
		var rels []upgrade.Release
		if err == nil {
			rels, err = src.Releases(ctx, 1)
		}
		gtkx.Post(func() {
			a.checking, a.checked, a.err = false, time.Now(), err
			if err != nil {
				log.Printf("about: %v", err)
			} else {
				a.latest, a.releases = latest, rels
			}
			if s.settings.win != nil {
				s.showAboutSettings()
			}
		})
	}()
}

// showAboutSettings fills the page from what is known.
func (s *shellUI) showAboutSettings() {
	a := &s.settings.about
	if s.settings.win == nil {
		return
	}
	v := s.opts.Version
	order, known := upgrade.Compare(v, a.latest)
	if upgrade.IsRelease(v) {
		a.version.SetText("inua " + strings.TrimPrefix(v, "v"))
	} else {
		a.version.SetText(i18n.Tf("inua %s, built from source", v))
	}
	a.daily.SetActive(s.cfg.Notify.Updates)
	a.check.SetSensitive(!a.checking && !a.upgrading)
	// A build from source comes before every release, so it can take one.
	a.upgrade.SetVisible(a.latest != "" && known && order < 0)
	a.upgrade.SetSensitive(!a.upgrading)
	a.upgrade.SetLabel(i18n.Tf("Upgrade to %s", strings.TrimPrefix(a.latest, "v")))
	switch {
	case a.upgrading:
		a.state.SetText(i18n.T("Upgrading… the shell restarts on the new version"))
	case a.checking:
		a.state.SetText(i18n.T("Checking…"))
	case a.said != "":
		a.state.SetText(a.said)
	case a.err != nil:
		a.state.SetText(i18n.Tf("Could not reach GitHub: %s", a.err.Error()))
	case a.latest == "":
		a.state.SetText(i18n.T("Not checked yet"))
	case !known:
		a.state.SetText(i18n.Tf("The latest release is %s; this version cannot be compared with it", a.latest))
	case order < 0 && !upgrade.IsRelease(v):
		a.state.SetText(i18n.Tf("inua %s is out; upgrading replaces this build from source", strings.TrimPrefix(a.latest, "v")))
	case order < 0:
		a.state.SetText(i18n.Tf("inua %s is out", strings.TrimPrefix(a.latest, "v")))
	case order == 0:
		a.state.SetText(i18n.Tf("Up to date: %s is the latest release (checked %s)", a.latest, a.checked.Format("15:04")))
	default:
		a.state.SetText(i18n.Tf("Newer than the latest release, %s", a.latest))
	}
	s.showChangelog()
}

// showChangelog lists the latest release's changes.
func (s *shellUI) showChangelog() {
	a := &s.settings.about
	gtkx.Clear(&a.changelog)
	if len(a.releases) == 0 {
		l := gtk.NewLabel(i18n.T("Shown once the releases are read"))
		if a.checking {
			l.SetText(i18n.T("Reading the releases…"))
		}
		l.AddCssClass("sub")
		l.SetXalign(0)
		gtkx.Append(&a.changelog, &l.Widget)
		return
	}
	for _, r := range a.releases[:1] {
		box := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		box.AddCssClass("row")
		box.AddCssClass("changelog-release")
		head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		tag := gtk.NewLabel(r.Tag)
		tag.AddCssClass("title")
		gtkx.Append(head, &tag.Widget)
		date := gtk.NewLabel(i18n.Time(r.Date.Local(), "Jan 2, 2006"))
		date.AddCssClass("readout")
		date.SetHexpand(true)
		date.SetXalign(0)
		gtkx.Append(head, &date.Widget)
		if r.Tag == s.opts.Version || "v"+s.opts.Version == r.Tag {
			now := gtk.NewLabel(i18n.T("installed"))
			now.AddCssClass("tag")
			gtkx.Append(head, &now.Widget)
		}
		link := gtk.NewButtonWithLabel(i18n.T("On GitHub"))
		link.AddCssClass("link")
		link.SetName("about_url:" + r.URL)
		link.ConnectClicked(&s.onAboutLink)
		gtkx.Append(head, &link.Widget)
		gtkx.Append(box, &head.Widget)
		for _, c := range upgrade.Changes(r.Body) {
			l := gtk.NewLabel("•  " + c)
			l.AddCssClass("sub")
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetSelectable(true)
			gtkx.Append(box, &l.Widget)
		}
		gtkx.Append(&a.changelog, &box.Widget)
	}
}

func (s *shellUI) bindAboutHandlers() {
	s.onAboutLink = func(b gtk.Button) {
		url := strings.TrimPrefix(b.GetName(), "about_url:")
		go func() {
			if err := exec.Command("xdg-open", url).Run(); err != nil {
				log.Printf("about: %v", err)
			}
		}()
	}
}
