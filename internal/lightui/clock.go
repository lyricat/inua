package lightui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/clock"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// alarmSound plays when the timer ends (the shell plays it for alarms).
const alarmSound = "/usr/share/sounds/freedesktop/stereo/alarm-clock-elapsed.oga"

// timerPresets are the timer's ready-made durations, in minutes.
var timerPresets = []int{1, 3, 5, 10, 15, 30}

type clockApp struct {
	c    *appui.Ctx
	data clock.Data
	mod  time.Time // the store file's time when last read

	pages                             gtk.Stack
	localTime, localSeconds           gtk.Label
	localDate, worldEmpty, alarmEmpty gtk.Label
	zoneSearch                        gtk.SearchEntry
	zoneResults, worldList, alarmList gtk.Box
	worldTimes                        map[string]*gtk.Label
	worldMarks                        map[string]*gtk.Box
	zones                             []string // all, for searching

	editor      gtk.Revealer
	alarmPick   *picker
	alarmLabel  gtk.Entry
	alarmRepeat gtk.Label
	alarmDays   [7]*gtk.ToggleButton
	alarmDelete gtk.Button
	editing     string // the alarm being edited; "" for a new one

	swTime         gtk.Label
	swStart, swLap gtk.Button
	swLaps         gtk.Box
	swRunning      bool
	swFrom         time.Time     // when the running stretch began
	swBefore       time.Duration // time from earlier stretches
	laps           []time.Duration

	timerSet, timerRun   gtk.Box
	timerPick            *picker
	timerLeft, timerEnds gtk.Label
	timerProgress        gtk.ProgressBar
	timerPause           gtk.Button
	timerEnd             time.Time     // while running
	timerTotal           time.Duration // as started
	timerRemaining       time.Duration // while paused
	timerOn, timerPaused bool

	lastSecond       int64
	lastCheck        time.Time
	onButton         func(gtk.Button)
	onSearch         func(gtk.SearchEntry)
	onAlarmSwitch    func(gtk.Switch, bool) bool
	onDay            func(gtk.ToggleButton)
	onTick, onReshow glib.SourceFunc
}

func (a *clockApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	win := &gtk.ApplicationWindow{}
	var alarmPicker, timerPicker, days, presets gtk.Box
	ui := c.Load("clock.ui")
	ui.Get("clock", win)
	ui.Get("pages", &a.pages)
	ui.Get("local_time", &a.localTime)
	ui.Get("local_seconds", &a.localSeconds)
	ui.Get("local_date", &a.localDate)
	ui.Get("zone_search", &a.zoneSearch)
	ui.Get("zone_results", &a.zoneResults)
	ui.Get("world_list", &a.worldList)
	ui.Get("world_empty", &a.worldEmpty)
	ui.Get("editor_reveal", &a.editor)
	ui.Get("alarm_picker", &alarmPicker)
	ui.Get("alarm_label", &a.alarmLabel)
	ui.Get("alarm_repeat", &a.alarmRepeat)
	ui.Get("alarm_days", &days)
	ui.Get("alarm_delete", &a.alarmDelete)
	ui.Get("alarm_list", &a.alarmList)
	ui.Get("alarm_empty", &a.alarmEmpty)
	ui.Get("sw_time", &a.swTime)
	ui.Get("sw_start", &a.swStart)
	ui.Get("sw_lap", &a.swLap)
	ui.Get("sw_laps", &a.swLaps)
	ui.Get("timer_set", &a.timerSet)
	ui.Get("timer_run", &a.timerRun)
	ui.Get("timer_picker", &timerPicker)
	ui.Get("timer_presets", &presets)
	ui.Get("timer_left", &a.timerLeft)
	ui.Get("timer_ends", &a.timerEnds)
	ui.Get("timer_progress", &a.timerProgress)
	ui.Get("timer_pause", &a.timerPause)
	buttons := map[string]*gtk.Button{}
	for _, id := range []string{"alarm_add", "alarm_save", "alarm_cancel", "alarm_delete", "sw_start", "sw_lap", "timer_start", "timer_pause", "timer_cancel"} {
		b := &gtk.Button{}
		ui.Get(id, b)
		b.SetName(id)
		buttons[id] = b
	}
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onSearch = func(e gtk.SearchEntry) { a.search(e.GetText()) }
	a.onAlarmSwitch = func(sw gtk.Switch, on bool) bool {
		a.setAlarmOn(strings.TrimPrefix(sw.GetName(), "alarm-on:"), on)
		return false
	}
	a.onDay = func(gtk.ToggleButton) { a.showRepeat() }
	a.onTick = func(uintptr) bool {
		a.tick()
		return true
	}
	a.onReshow = func(uintptr) bool {
		a.showAlarms()
		return false
	}
	for _, b := range buttons {
		b.ConnectClicked(&a.onButton)
	}
	a.zoneSearch.ConnectSearchChanged(&a.onSearch)

	a.alarmPick = newPicker(&alarmPicker, []int{23, 59}, []bool{true, true})
	alarmPicker.AddCssClass("large")
	a.timerPick = newPicker(&timerPicker, []int{23, 59, 59}, []bool{false, true, true}, i18n.T("HRS"), i18n.T("MIN"), i18n.T("SEC"))
	timerPicker.AddCssClass("large")
	a.timerPick.set(1, 5)
	for i, name := range []string{i18n.T("Su"), i18n.T("Mo"), i18n.T("Tu"), i18n.T("We"), i18n.T("Th"), i18n.T("Fr"), i18n.T("Sa")} {
		t := gtk.NewToggleButtonWithLabel(name)
		t.SetTooltipText(i18n.T(time.Weekday(i).String()))
		t.ConnectToggled(&a.onDay)
		a.alarmDays[i] = t
		gtkx.Append(&days, &t.Widget)
	}
	for _, m := range timerPresets {
		b := gtk.NewButtonWithLabel(i18n.Tf("%d min", m))
		b.AddCssClass("chip")
		b.SetName("preset:" + strconv.Itoa(m))
		b.ConnectClicked(&a.onButton)
		gtkx.Append(&presets, &b.Widget)
	}

	a.zones = clock.Zones()
	a.load()
	a.showWorld()
	a.showAlarms()
	a.showLaps()
	a.tick()
	menuWindow(c, win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "clock-world", Label: "World clocks", Activate: func(gio.SimpleAction, uintptr) { a.pages.SetVisibleChildName("world") }}, {Name: "clock-alarms", Label: "Alarms", Activate: func(gio.SimpleAction, uintptr) { a.pages.SetVisibleChildName("alarms") }}, {Name: "clock-stopwatch", Label: "Stopwatch", Activate: func(gio.SimpleAction, uintptr) { a.pages.SetVisibleChildName("stopwatch") }}, {Name: "clock-timer", Label: "Timer", Activate: func(gio.SimpleAction, uintptr) { a.pages.SetVisibleChildName("timer") }}}}}}})
	glib.TimeoutAdd(50, &a.onTick, 0)
	return win
}

func (a *clockApp) ThemeChanged() {}

func (a *clockApp) ShowPage(name string) {
	if a.pages.GetChildByName(name) != nil {
		a.pages.SetVisibleChildName(name)
	}
}

func (a *clockApp) load() {
	if err := store.Load(clock.StoreName, &a.data); err != nil {
		log.Printf("clock: %v", err)
	}
	if st, err := os.Stat(store.Path(clock.StoreName)); err == nil {
		a.mod = st.ModTime()
	}
}

func (a *clockApp) save() {
	if err := store.Save(clock.StoreName, a.data); err != nil {
		log.Printf("clock: %v", err)
	}
	if st, err := os.Stat(store.Path(clock.StoreName)); err == nil {
		a.mod = st.ModTime()
	}
}

// tick keeps the clocks, the stopwatch and the timer current, and reads
// the file again when the shell changed it (a once alarm went off).
func (a *clockApp) tick() {
	now := time.Now()
	if a.swRunning {
		a.swTime.SetText(clock.Stopwatch(a.swElapsed(now)))
	}
	if a.timerOn && !a.timerPaused {
		left := time.Until(a.timerEnd)
		a.timerLeft.SetText(clock.Countdown(left))
		a.timerProgress.SetFraction(max(0, float64(left)/float64(a.timerTotal)))
		if left <= 0 {
			a.timerDone()
		}
	}
	if s := now.Unix(); s != a.lastSecond {
		a.lastSecond = s
		a.localTime.SetText(now.Format("15:04"))
		a.localSeconds.SetText(now.Format("05"))
		a.localDate.SetText(strings.ToUpper(i18n.Time(now, "Monday, January 2") + " · " + localCity()))
		for zone, l := range a.worldTimes {
			if loc, err := time.LoadLocation(zone); err == nil {
				there := now.In(loc)
				l.SetText(there.Format("15:04"))
				markDay(a.worldMarks[zone], there)
			}
		}
	}
	if now.Sub(a.lastCheck) > 2*time.Second {
		a.lastCheck = now
		if st, err := os.Stat(store.Path(clock.StoreName)); err == nil && !st.ModTime().Equal(a.mod) {
			a.load()
			a.showWorld()
			a.showAlarms()
		}
	}
}

// localCity names the local time zone's city.
func localCity() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return clock.City(tz)
	}
	if p, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(p, "zoneinfo/"); i >= 0 {
			return clock.City(p[i+len("zoneinfo/"):])
		}
	}
	name, _ := time.Now().Zone()
	return name
}

// markDay fills a city's square in daytime (6 to 18) and hollows it at
// night.
func markDay(mark *gtk.Box, t time.Time) {
	if mark == nil {
		return
	}
	if h := t.Hour(); h >= 6 && h < 18 {
		mark.RemoveCssClass("night")
		mark.SetTooltipText(i18n.T("Day"))
	} else {
		mark.AddCssClass("night")
		mark.SetTooltipText(i18n.T("Night"))
	}
}

func (a *clockApp) button(name string) {
	switch {
	case name == "alarm_add":
		a.editAlarm("")
	case name == "alarm_save":
		a.saveAlarm()
	case name == "alarm_cancel":
		a.editor.SetRevealChild(false)
	case name == "alarm_delete":
		a.data.Alarms = slices.DeleteFunc(a.data.Alarms, func(al clock.Alarm) bool { return al.ID == a.editing })
		a.editor.SetRevealChild(false)
		a.save()
		a.showAlarms()
	case strings.HasPrefix(name, "alarm-edit:"):
		a.editAlarm(strings.TrimPrefix(name, "alarm-edit:"))
	case strings.HasPrefix(name, "zone-add:"):
		zone := strings.TrimPrefix(name, "zone-add:")
		if !slices.Contains(a.data.Zones, zone) {
			a.data.Zones = append(a.data.Zones, zone)
			a.save()
		}
		a.zoneSearch.SetText("")
		a.showWorld()
	case strings.HasPrefix(name, "zone-remove:"):
		zone := strings.TrimPrefix(name, "zone-remove:")
		a.data.Zones = slices.DeleteFunc(a.data.Zones, func(z string) bool { return z == zone })
		a.save()
		a.showWorld()
	case name == "sw_start":
		a.toggleStopwatch()
	case name == "sw_lap":
		a.lapOrReset()
	case strings.HasPrefix(name, "preset:"):
		m, _ := strconv.Atoi(strings.TrimPrefix(name, "preset:"))
		a.startTimer(time.Duration(m) * time.Minute)
	case name == "timer_start":
		v := a.timerPick.value
		d := time.Duration(v[0])*time.Hour + time.Duration(v[1])*time.Minute + time.Duration(v[2])*time.Second
		if d > 0 {
			a.startTimer(d)
		}
	case name == "timer_pause":
		a.pauseTimer()
	case name == "timer_cancel":
		a.stopTimer()
	}
}

// --- world clocks ---

func (a *clockApp) search(q string) {
	gtkx.Clear(&a.zoneResults)
	q = strings.TrimSpace(q)
	a.zoneResults.SetVisible(q != "")
	a.worldList.SetVisible(q == "")
	a.worldEmpty.SetVisible(q == "" && len(a.data.Zones) == 0)
	if q == "" {
		return
	}
	n := 0
	for _, z := range a.zones {
		if n == 12 {
			break
		}
		if !clock.Match(z, q) {
			continue
		}
		n++
		gtkx.Append(&a.zoneResults, &rowButton("zone-add:"+z, &text(clock.City(z), clock.Region(z)).Widget, &a.onButton).Widget)
	}
	if n == 0 {
		l := gtk.NewLabel(i18n.T("No city by that name"))
		l.AddCssClass("empty")
		gtkx.Append(&a.zoneResults, &l.Widget)
	}
}

func (a *clockApp) showWorld() {
	gtkx.Clear(&a.worldList)
	a.worldTimes = map[string]*gtk.Label{}
	a.worldMarks = map[string]*gtk.Box{}
	now := time.Now()
	for _, z := range a.data.Zones {
		loc, err := time.LoadLocation(z)
		if err != nil {
			continue
		}
		there := now.In(loc)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
		row.AddCssClass("row")
		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("daymark")
		mark.SetValign(gtk.AlignCenterValue)
		markDay(mark, there)
		a.worldMarks[z] = mark
		gtkx.Append(row, &mark.Widget)
		t := text(clock.City(z), clock.Difference(loc, now))
		t.SetHexpand(true)
		gtkx.Append(row, &t.Widget)
		tl := gtk.NewLabel(there.Format("15:04"))
		tl.AddCssClass("row-time")
		a.worldTimes[z] = tl
		gtkx.Append(row, &tl.Widget)
		gtkx.Append(row, &plainIcon("zone-remove:"+z, "window-close-symbolic", i18n.Tf("Remove %s", clock.City(z)), &a.onButton).Widget)
		gtkx.Append(&a.worldList, &row.Widget)
	}
	a.worldEmpty.SetVisible(len(a.data.Zones) == 0 && a.zoneSearch.GetText() == "")
}

// --- alarms ---

func (a *clockApp) showAlarms() {
	gtkx.Clear(&a.alarmList)
	alarms := slices.Clone(a.data.Alarms)
	slices.SortFunc(alarms, func(x, y clock.Alarm) int { return (x.Hour*60 + x.Minute) - (y.Hour*60 + y.Minute) })
	for _, al := range alarms {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
		row.AddCssClass("row")
		if !al.On {
			row.AddCssClass("off")
		}
		box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		t := gtk.NewLabel(fmt.Sprintf("%02d:%02d", al.Hour, al.Minute))
		t.AddCssClass("alarm-time")
		t.SetXalign(0)
		gtkx.Append(box, &t.Widget)
		sub := clock.DaysLabel(al.Days)
		if al.Label != "" {
			sub = al.Label + " · " + sub
		}
		s := gtk.NewLabel(sub)
		s.AddCssClass("sub")
		s.SetXalign(0)
		gtkx.Append(box, &s.Widget)
		edit := rowButton("alarm-edit:"+al.ID, &box.Widget, &a.onButton)
		edit.SetHexpand(true)
		edit.SetTooltipText(i18n.T("Edit"))
		gtkx.Append(row, &edit.Widget)
		sw := gtk.NewSwitch()
		sw.SetName("alarm-on:" + al.ID)
		sw.SetValign(gtk.AlignCenterValue)
		sw.SetActive(al.On)
		sw.ConnectStateSet(&a.onAlarmSwitch)
		gtkx.Append(row, &sw.Widget)
		gtkx.Append(&a.alarmList, &row.Widget)
	}
	a.alarmEmpty.SetVisible(len(alarms) == 0)
}

func (a *clockApp) editAlarm(id string) {
	a.editing = id
	al := clock.Alarm{Hour: 7}
	if i := slices.IndexFunc(a.data.Alarms, func(al clock.Alarm) bool { return al.ID == id }); i >= 0 {
		al = a.data.Alarms[i]
	}
	a.alarmPick.set(0, al.Hour)
	a.alarmPick.set(1, al.Minute)
	a.alarmLabel.SetText(al.Label)
	for i, t := range a.alarmDays {
		t.SetActive(al.Days[i])
	}
	a.showRepeat()
	a.alarmDelete.SetVisible(id != "")
	a.editor.SetRevealChild(true)
}

// showRepeat describes the days chosen in the editor.
func (a *clockApp) showRepeat() {
	var days [7]bool
	for i, t := range a.alarmDays {
		days[i] = t != nil && t.GetActive()
	}
	a.alarmRepeat.SetText(clock.DaysLabel(days))
}

func (a *clockApp) saveAlarm() {
	al := clock.Alarm{
		ID: a.editing, Hour: a.alarmPick.value[0], Minute: a.alarmPick.value[1],
		Label: strings.TrimSpace(a.alarmLabel.GetText()), On: true,
	}
	for i, t := range a.alarmDays {
		al.Days[i] = t.GetActive()
	}
	if i := slices.IndexFunc(a.data.Alarms, func(x clock.Alarm) bool { return x.ID == al.ID }); al.ID != "" && i >= 0 {
		a.data.Alarms[i] = al
	} else {
		al.ID = newID()
		a.data.Alarms = append(a.data.Alarms, al)
	}
	a.editor.SetRevealChild(false)
	a.save()
	a.showAlarms()
}

func (a *clockApp) setAlarmOn(id string, on bool) {
	for i := range a.data.Alarms {
		if a.data.Alarms[i].ID == id && a.data.Alarms[i].On != on {
			a.data.Alarms[i].On = on
			a.save()
			glib.IdleAdd(&a.onReshow, 0) // not while the switch's signal runs
			return
		}
	}
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- stopwatch ---

func (a *clockApp) swElapsed(now time.Time) time.Duration {
	if a.swRunning {
		return a.swBefore + now.Sub(a.swFrom)
	}
	return a.swBefore
}

func (a *clockApp) toggleStopwatch() {
	now := time.Now()
	if a.swRunning {
		a.swBefore += now.Sub(a.swFrom)
		a.swRunning = false
		a.swStart.SetLabel(i18n.T("Start"))
		a.swLap.SetLabel(i18n.T("Reset"))
		a.swTime.SetText(clock.Stopwatch(a.swBefore))
	} else {
		a.swFrom, a.swRunning = now, true
		a.swStart.SetLabel(i18n.T("Stop"))
		a.swLap.SetLabel(i18n.T("Lap"))
	}
	a.swLap.SetSensitive(true)
}

func (a *clockApp) lapOrReset() {
	if !a.swRunning { // reset
		a.swBefore, a.laps = 0, nil
		a.swTime.SetText(clock.Stopwatch(0))
		a.swLap.SetLabel(i18n.T("Lap"))
		a.swLap.SetSensitive(false)
		a.showLaps()
		return
	}
	var before time.Duration
	for _, l := range a.laps {
		before += l
	}
	a.laps = append(a.laps, a.swElapsed(time.Now())-before)
	a.showLaps()
}

// showLaps lists the laps, newest first, the fastest and the slowest
// marked once there are three.
func (a *clockApp) showLaps() {
	gtkx.Clear(&a.swLaps)
	fast, slow := -1, -1
	if len(a.laps) >= 3 {
		fast, slow = 0, 0
		for i, l := range a.laps {
			if l < a.laps[fast] {
				fast = i
			}
			if l > a.laps[slow] {
				slow = i
			}
		}
	}
	for i := len(a.laps) - 1; i >= 0; i-- {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
		row.AddCssClass("row")
		row.AddCssClass("compact")
		switch i {
		case fast:
			row.AddCssClass("fastest")
		case slow:
			row.AddCssClass("slowest")
		}
		n := gtk.NewLabel(i18n.Tf("LAP %d", i+1))
		n.AddCssClass("kicker")
		n.SetHexpand(true)
		n.SetXalign(0)
		gtkx.Append(row, &n.Widget)
		d := gtk.NewLabel(clock.Stopwatch(a.laps[i]))
		d.AddCssClass("lap-time")
		gtkx.Append(row, &d.Widget)
		gtkx.Append(&a.swLaps, &row.Widget)
	}
}

// --- timer ---

func (a *clockApp) startTimer(d time.Duration) {
	a.timerOn, a.timerPaused = true, false
	a.timerTotal = d
	a.timerEnd = time.Now().Add(d)
	a.timerSet.SetVisible(false)
	a.timerRun.SetVisible(true)
	a.timerPause.SetLabel(i18n.T("Pause"))
	a.timerLeft.SetText(clock.Countdown(d))
	a.timerProgress.SetFraction(1)
	a.timerEnds.SetText(i18n.Tf("ENDS AT %s", a.timerEnd.Format("15:04")))
}

func (a *clockApp) pauseTimer() {
	if a.timerPaused {
		a.timerPaused = false
		a.timerEnd = time.Now().Add(a.timerRemaining)
		a.timerPause.SetLabel(i18n.T("Pause"))
		a.timerEnds.SetText(i18n.Tf("ENDS AT %s", a.timerEnd.Format("15:04")))
		return
	}
	a.timerPaused = true
	a.timerRemaining = time.Until(a.timerEnd)
	a.timerPause.SetLabel(i18n.T("Resume"))
	a.timerEnds.SetText(i18n.T("PAUSED"))
}

func (a *clockApp) stopTimer() {
	a.timerOn = false
	a.timerSet.SetVisible(true)
	a.timerRun.SetVisible(false)
}

func (a *clockApp) timerDone() {
	a.stopTimer()
	n := gio.NewNotification(i18n.T("Timer done"))
	n.SetBody(i18n.T("Your timer has ended."))
	n.SetPriority(gio.GNotificationPriorityUrgentValue)
	a.c.App.SendNotification("timer", n)
	n.Unref()
	go func() {
		if err := exec.Command("pw-play", alarmSound).Run(); err != nil {
			log.Printf("clock: sound: %v", err)
		}
	}()
}
