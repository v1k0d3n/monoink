package screens

import (
	"testing"
	"time"
)

func TestYearProgressAt(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 12, 0, 0, 0, time.UTC) }
	cases := []struct {
		name string
		at   time.Time
		want YearProgress
	}{
		{"first day, week from previous ISO year", d(2021, time.January, 1), YearProgress{Day: 1, Days: 365, Week: 53, Weeks: 53}},
		{"leap day", d(2024, time.February, 29), YearProgress{Day: 60, Days: 366, Week: 9, Weeks: 52}},
		{"last day of a leap year, week 1 of next ISO year", d(2024, time.December, 31), YearProgress{Day: 366, Days: 366, Week: 1, Weeks: 52}},
		{"last day of a 53-week year", d(2026, time.December, 31), YearProgress{Day: 365, Days: 365, Week: 53, Weeks: 53}},
		{"ordinary day", d(2026, time.October, 7), YearProgress{Day: 280, Days: 365, Week: 41, Weeks: 53}},
		{"first day, week 1", d(2024, time.January, 1), YearProgress{Day: 1, Days: 366, Week: 1, Weeks: 52}},
	}
	for _, c := range cases {
		if got := YearProgressAt(c.at); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestYearProgressUsesLocalDate(t *testing.T) {
	// 23:30 on 31 Dec in New York is already 1 Jan in UTC; the display
	// must follow the user's local calendar.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	got := YearProgressAt(time.Date(2025, time.December, 31, 23, 30, 0, 0, ny))
	if got.Day != 365 || got.Days != 365 {
		t.Fatalf("got %+v", got)
	}
}

func TestYearProgressString(t *testing.T) {
	y := YearProgress{Day: 280, Days: 365, Week: 41, Weeks: 53}
	if got := y.String(); got != "Day 280 of 365 · Week 41" {
		t.Fatalf("got %q", got)
	}
	if f := y.Fraction(); f < 0.767 || f > 0.768 {
		t.Fatalf("fraction %v", f)
	}
}
