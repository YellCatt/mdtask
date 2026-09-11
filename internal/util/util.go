package util

import (
	"errors"
	"strings"
	"time"
)

var ErrNoDate = errors.New("no date")

func Today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func MondayOf(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t.AddDate(0, 0, 1-wd)
}

func PriorityRank(p string) int {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "P0":
		return 5
	case "P1":
		return 4
	case "P2":
		return 3
	case "P3":
		return 2
	case "P4":
		return 1
	}
	return 0
}

func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, ErrNoDate
	}
	layouts := []string{"2006-01-02", "2006/01/02", "2006.01.02", "01-02", "01/02"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			if l == "01-02" || l == "01/02" {
				t = time.Date(time.Now().Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			}
			return t, nil
		}
	}
	return time.Time{}, ErrNoDate
}