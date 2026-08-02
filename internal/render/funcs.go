package render

import (
	"text/template"
	"time"
)

// FuncMap returns the set of template functions available to all templates.
// All functions are pure and perform no I/O.
// The now parameter is the reference time used by humanize; it must be
// provided to create closures with the correct reference point.
func FuncMap(now time.Time) template.FuncMap {
	return template.FuncMap{
		"humanize": func(t time.Time) string {
			return humanize(t, now)
		},
	}
}

// humanize formats t as a relative time string (e.g., "1d ago", "2h ago", "now")
// from the reference time now, after flattening both to midnight UTC.
// Invariant 4 requires this: otherwise an hourly cron produces a different
// README every run, committing on every run.
func humanize(t time.Time, now time.Time) string {
	// Flatten both to midnight UTC
	t = t.Truncate(24 * time.Hour)
	now = now.Truncate(24 * time.Hour)

	// Handle zero time
	if t.IsZero() {
		return ""
	}

	diff := now.Sub(t)

	// If times are equal after flattening, it's "now"
	if diff == 0 {
		return "now"
	}

	// If t is in the future
	if diff < 0 {
		diff = -diff
		days := int(diff.Hours() / 24)
		if days == 0 {
			return "today"
		}
		if days == 1 {
			return "tomorrow"
		}
		return "in the future"
	}

	// t is in the past
	days := int(diff.Hours() / 24)
	hours := int(diff.Hours())

	if days == 0 && hours == 0 {
		return "now"
	}
	if days == 0 {
		return "today"
	}
	if days == 1 {
		return "1d ago"
	}
	if days < 7 {
		return formatDays(days)
	}
	if days < 30 {
		weeks := days / 7
		return formatWeeks(weeks)
	}
	months := days / 30
	return formatMonths(months)
}

func formatDays(days int) string {
	if days == 1 {
		return "1d ago"
	}
	if days < 1000 {
		// Use printf-style formatting for compatibility
		result := ""
		d := days
		for d > 0 {
			result = string(rune('0'+(d%10))) + result
			d /= 10
		}
		return result + "d ago"
	}
	return "long ago"
}

func formatWeeks(weeks int) string {
	if weeks == 1 {
		return "1w ago"
	}
	if weeks < 100 {
		result := ""
		w := weeks
		for w > 0 {
			result = string(rune('0'+(w%10))) + result
			w /= 10
		}
		return result + "w ago"
	}
	return "long ago"
}

func formatMonths(months int) string {
	if months == 1 {
		return "1mo ago"
	}
	if months < 12 {
		result := ""
		m := months
		for m > 0 {
			result = string(rune('0'+(m%10))) + result
			m /= 10
		}
		return result + "mo ago"
	}
	years := months / 12
	if years == 1 {
		return "1y ago"
	}
	result := ""
	y := years
	for y > 0 {
		result = string(rune('0'+(y%10))) + result
		y /= 10
	}
	return result + "y ago"
}
