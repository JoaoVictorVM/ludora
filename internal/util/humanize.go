// Package util holds small presentation helpers shared by the view layer.
package util

import (
	"fmt"
	"time"
)

// absoluteAfter is the age past which a relative string stops being useful and
// an actual date reads better.
const absoluteAfter = 30 * 24 * time.Hour

// RelativeTime renders a timestamp as a human-readable string in Brazilian
// Portuguese. It is computed at render time on the server: the value goes stale
// until the next page load, which is the trade-off for keeping the page free of
// client-side JavaScript.
func RelativeTime(t, now time.Time) string {
	elapsed := now.Sub(t)

	switch {
	case elapsed < 0:
		return "agora mesmo"
	case elapsed < time.Minute:
		return "agora mesmo"
	case elapsed < time.Hour:
		return pluralize(int(elapsed.Minutes()), "minuto", "minutos")
	case elapsed < 24*time.Hour:
		return pluralize(int(elapsed.Hours()), "hora", "horas")
	case elapsed < absoluteAfter:
		return pluralize(int(elapsed.Hours()/24), "dia", "dias")
	default:
		return t.Format("02/01/2006")
	}
}

func pluralize(value int, singular, plural string) string {
	if value == 1 {
		return fmt.Sprintf("há 1 %s", singular)
	}

	return fmt.Sprintf("há %d %s", value, plural)
}
