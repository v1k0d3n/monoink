package screens

import (
	"fmt"
	"time"
)

// YearProgress describes how far through the year a moment is.
type YearProgress struct {
	Day   int // day of the year, 1-based
	Days  int // 365 or 366
	Week  int // ISO 8601 week number (matches `date +%V`)
	Weeks int // weeks in that ISO week-year, 52 or 53
}

// YearProgressAt computes the year progress for t in t's location.
//
// The ISO week can belong to a neighbouring year: 1 January 2021 is in
// week 53 of 2020, and 31 December 2024 is in week 1 of 2025. Weeks is
// counted for the week's own ISO year.
func YearProgressAt(t time.Time) YearProgress {
	isoYear, week := t.ISOWeek()
	days := 365
	if time.Date(t.Year(), time.December, 31, 0, 0, 0, 0, t.Location()).YearDay() == 366 {
		days = 366
	}
	// 28 December always falls in the last ISO week of its year.
	_, weeks := time.Date(isoYear, time.December, 28, 0, 0, 0, 0, t.Location()).ISOWeek()
	return YearProgress{Day: t.YearDay(), Days: days, Week: week, Weeks: weeks}
}

// Fraction is the share of the year completed, counting today as done.
func (y YearProgress) Fraction() float64 { return float64(y.Day) / float64(y.Days) }

func (y YearProgress) String() string {
	return fmt.Sprintf("Day %d of %d · Week %d", y.Day, y.Days, y.Week)
}
