package i18n

import (
	"strings"
	"sync"
	"time"
)

var months = []string{M("January"), M("February"), M("March"), M("April"), M("May"), M("June"), M("July"), M("August"), M("September"), M("October"), M("November"), M("December")}
var shortMonths = []string{M("Jan"), M("Feb"), M("Mar"), M("Apr"), M("May"), M("Jun"), M("Jul"), M("Aug"), M("Sep"), M("Oct"), M("Nov"), M("Dec")}
var weekdays = []string{M("Sunday"), M("Monday"), M("Tuesday"), M("Wednesday"), M("Thursday"), M("Friday"), M("Saturday")}
var shortWeekdays = []string{M("Sun"), M("Mon"), M("Tue"), M("Wed"), M("Thu"), M("Fri"), M("Sat")}

var scopedTimeCatalogs struct {
	once sync.Once
	cats map[string]*catalog
}

func timeCatalog(lang string) *catalog {
	switch lang {
	case "en":
		return nil
	case "zh_CN", "zh_TW", "ja", "ko", "es", "fr":
	default:
		return nil
	}
	scopedTimeCatalogs.once.Do(func() {
		scopedTimeCatalogs.cats = make(map[string]*catalog, 6)
		for _, name := range []string{"zh_CN", "zh_TW", "ja", "ko", "es", "fr"} {
			data, err := locales.ReadFile("locales/" + name + ".po")
			if err != nil {
				continue
			}
			c, err := parse(string(data))
			if err == nil {
				c.lang = name
				scopedTimeCatalogs.cats[name] = c
			}
		}
	})
	return scopedTimeCatalogs.cats[lang]
}

func timeTranslation(c *catalog, msgid string) string {
	if c != nil {
		if s := c.msgs[msgid]; len(s) > 0 && s[0] != "" {
			return s[0]
		}
	}
	return msgid
}

// TimeIn formats a time in a clock-specific language without changing the
// language used by the rest of the application. Empty and "auto" follow it.
func TimeIn(lang string, t time.Time, layout string) string {
	if lang == "" || lang == "auto" {
		return Time(t, layout)
	}
	translate := func(s string) string { return timeTranslation(timeCatalog(lang), s) }
	switch layout {
	case "January", "Jan", "Monday", "Mon", "PM", "pm":
	default:
		layout = translate(layout)
	}
	layout = strings.NewReplacer("January", "\x01", "Jan", "\x02", "Monday", "\x03", "Mon", "\x04", "PM", "\x05", "pm", "\x06").Replace(layout)
	period := translate("AM")
	if t.Hour() >= 12 {
		period = translate("PM")
	}
	return strings.NewReplacer("\x01", translate(months[t.Month()-1]), "\x02", translate(shortMonths[t.Month()-1]), "\x03", translate(weekdays[t.Weekday()]), "\x04", translate(shortWeekdays[t.Weekday()]), "\x05", period, "\x06", strings.ToLower(period)).Replace(t.Format(layout))
}

// Time formats a translated Go time layout, including localized month and
// weekday names. Numeric layouts used in filenames and protocols use time.Format.
func Time(t time.Time, layout string) string {
	switch layout {
	case "January", "Jan", "Monday", "Mon", "PM", "pm":
	default:
		layout = T(layout)
	}
	layout = strings.NewReplacer("January", "\x01", "Jan", "\x02", "Monday", "\x03", "Mon", "\x04", "PM", "\x05", "pm", "\x06").Replace(layout)
	period := T("AM")
	if t.Hour() >= 12 {
		period = T("PM")
	}
	return strings.NewReplacer("\x01", T(months[t.Month()-1]), "\x02", T(shortMonths[t.Month()-1]), "\x03", T(weekdays[t.Weekday()]), "\x04", T(shortWeekdays[t.Weekday()]), "\x05", period, "\x06", strings.ToLower(period)).Replace(t.Format(layout))
}
