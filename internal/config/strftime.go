package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// Strftime formats t like strftime(3), for the subset a clock needs:
//
//	%a %A  weekday, short/long      %H %I  hour, 24h/12h (00–23, 01–12)
//	%b %B  month name, short/long   %M %S  minute, second
//	%d %e  day (01–31, space-padded) %p    AM/PM
//	%m     month (01–12)            %Y %y  year, 4/2 digits
//	%j     day of the year          %Z %z  zone name/offset
//	%F %T %R  %Y-%m-%d, %H:%M:%S, %H:%M
//	%%     a percent sign
//
// A '-' after % drops the padding (%-d, %-H, %-I, %-m). Names follow the interface language.
// Unknown directives are kept as written.
// Strftime formats with the application's current date language.
func Strftime(format string, t time.Time) string {
	return StrftimeIn(format, t, "auto")
}

// StrftimeIn formats with a clock-specific date language.
func StrftimeIn(format string, t time.Time, lang string) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 == len(format) {
			b.WriteByte(c)
			continue
		}
		i++
		pad := true
		if format[i] == '-' && i+1 < len(format) {
			pad = false
			i++
		}
		num := func(v, width int) {
			if pad {
				fmt.Fprintf(&b, "%0*d", width, v)
			} else {
				fmt.Fprint(&b, v)
			}
		}
		hour12 := t.Hour() % 12
		if hour12 == 0 {
			hour12 = 12
		}
		switch format[i] {
		case 'a':
			b.WriteString(i18n.TimeIn(lang, t, "Mon"))
		case 'A':
			b.WriteString(i18n.TimeIn(lang, t, "Monday"))
		case 'b', 'h':
			b.WriteString(i18n.TimeIn(lang, t, "Jan"))
		case 'B':
			b.WriteString(i18n.TimeIn(lang, t, "January"))
		case 'd':
			num(t.Day(), 2)
		case 'e':
			if pad {
				fmt.Fprintf(&b, "%2d", t.Day())
			} else {
				fmt.Fprint(&b, t.Day())
			}
		case 'm':
			num(int(t.Month()), 2)
		case 'Y':
			fmt.Fprint(&b, t.Year())
		case 'y':
			num(t.Year()%100, 2)
		case 'j':
			num(t.YearDay(), 3)
		case 'H':
			num(t.Hour(), 2)
		case 'I':
			num(hour12, 2)
		case 'M':
			num(t.Minute(), 2)
		case 'S':
			num(t.Second(), 2)
		case 'p':
			b.WriteString(i18n.TimeIn(lang, t, "PM"))
		case 'Z':
			b.WriteString(t.Format("MST"))
		case 'z':
			b.WriteString(t.Format("-0700"))
		case 'F':
			b.WriteString(t.Format("2006-01-02"))
		case 'T':
			b.WriteString(t.Format("15:04:05"))
		case 'R':
			b.WriteString(t.Format("15:04"))
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			if !pad {
				b.WriteByte('-')
			}
			b.WriteByte(format[i])
		}
	}
	return b.String()
}

// HasSeconds reports whether a Strftime format shows seconds, so the clock
// knows to redraw every second rather than every minute.
func HasSeconds(format string) bool {
	return strings.Contains(format, "%S") || strings.Contains(format, "%-S") || strings.Contains(format, "%T")
}
