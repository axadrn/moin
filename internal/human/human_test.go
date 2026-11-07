package human

import (
	"testing"
	"time"
)

func TestAgo(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		age  time.Duration
		want string
	}{
		{-time.Minute, "just now"},
		{0, "just now"},
		{59 * time.Second, "just now"},
		{time.Minute, "1m ago"},
		{4*time.Minute + 50*time.Second, "4m ago"},
		{59 * time.Minute, "59m ago"},
		{time.Hour, "1h ago"},
		{47 * time.Hour, "47h ago"},
		{48 * time.Hour, "2d ago"},
		{59 * 24 * time.Hour, "59d ago"},
		{60 * 24 * time.Hour, "Sep 8, 2026"},
	} {
		if got := Ago(now.Add(-test.age), now); got != test.want {
			t.Errorf("Ago(now - %v) = %q, want %q", test.age, got, test.want)
		}
	}
}

func TestSize(t *testing.T) {
	for _, test := range []struct {
		bytes float64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{10 * 1024, "10 KB"},
		{512 << 20, "512 MB"},
		{1.5 * (1 << 30), "1.5 GB"},
		{38 << 30, "38 GB"},
		{2 << 40, "2.0 TB"},
	} {
		if got := Size(test.bytes); got != test.want {
			t.Errorf("Size(%v) = %q, want %q", test.bytes, got, test.want)
		}
	}
}

func TestDuration(t *testing.T) {
	for _, test := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0ms"},
		{0, "0ms"},
		{850 * time.Millisecond, "850ms"},
		{16 * time.Second, "16s"},
		{2*time.Minute + 5*time.Second, "2m 5s"},
		{2 * time.Minute, "2m"},
		{3*time.Hour + 10*time.Minute, "3h 10m"},
		{3 * time.Hour, "3h"},
		{47*time.Hour + 59*time.Minute, "47h 59m"},
		{76 * time.Hour, "3d 4h"},
		{72 * time.Hour, "3d"},
	} {
		if got := Duration(test.d); got != test.want {
			t.Errorf("Duration(%v) = %q, want %q", test.d, got, test.want)
		}
	}
}
