package util

import (
	"testing"
	"time"
)

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, time.August, 2, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "seconds ago", at: now.Add(-30 * time.Second), want: "agora mesmo"},
		{name: "one minute", at: now.Add(-time.Minute), want: "há 1 minuto"},
		{name: "several minutes", at: now.Add(-45 * time.Minute), want: "há 45 minutos"},
		{name: "one hour", at: now.Add(-time.Hour), want: "há 1 hora"},
		{name: "several hours", at: now.Add(-5 * time.Hour), want: "há 5 horas"},
		{name: "one day", at: now.Add(-24 * time.Hour), want: "há 1 dia"},
		{name: "several days", at: now.Add(-3 * 24 * time.Hour), want: "há 3 dias"},
		{name: "past the relative window", at: now.Add(-31 * 24 * time.Hour), want: "02/07/2026"},
		// A clock skew between the database and the server must not render
		// something like "há -2 minutos".
		{name: "future timestamp", at: now.Add(2 * time.Minute), want: "agora mesmo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RelativeTime(tt.at, now); got != tt.want {
				t.Errorf("RelativeTime = %q, want %q", got, tt.want)
			}
		})
	}
}
