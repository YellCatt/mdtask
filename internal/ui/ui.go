package ui

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"mdtask/internal/store"
)

const (
	cReset = "\033[0m"
	cDim   = "\033[2m"
	cGray  = "\033[90m"
	cRed   = "\033[31m"
	cGreen = "\033[32m"
	cYel   = "\033[33m"
	cBlue  = "\033[34m"
	cBold  = "\033[1m"
)

var useColor bool

func InitColor(color string) {
	switch color {
	case "always":
		useColor = true
		return
	case "never":
		useColor = false
		return
	}
	if os.Getenv("NO_COLOR") != "" {
		return
	}
	if os.Getenv("MDTASK_COLOR") != "" {
		useColor = true
		return
	}
	if runtime.GOOS == "windows" {
		return
	}
	fi, err := os.Stdout.Stat()
	useColor = err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func paint(c, s string) string {
	if c == "" || !useColor || s == "" {
		return s
	}
	return c + s + cReset
}

func Color(c, s string) string { return paint(c, s) }

// ---------- 终端宽度 ----------

func runeWidth(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r >= 0xfe00 && r <= 0xfe0f:
		return 0
	case r >= 0x1f000 && r <= 0x1faff,
		r >= 0x2600 && r <= 0x27bf,
		r >= 0x2b00 && r <= 0x2bff,
		r >= 0x2300 && r <= 0x23ff:
		return 2
	}
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x20000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}

func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func pad(s string, w int) string {
	n := w - dispWidth(s)
	if n < 0 {
		n = 0
	}
	return s + strings.Repeat(" ", n)
}

func Truncate(s string, max int) string {
	if dispWidth(s) <= max {
		return s
	}
	w := 0
	out := make([]rune, 0, max)
	for _, r := range s {
		d := runeWidth(r)
		if w+d > max-1 {
			break
		}
		w += d
		out = append(out, r)
	}
	return string(out) + "…"
}

func DueColor(t store.Task) string {
	if t.Due == "" {
		return ""
	}
	if t.Closed() {
		return ""
	}
	d, err := store.ParseDate(t.Due)
	if err != nil {
		return ""
	}
	today := store.Today()
	switch {
	case d.Before(today):
		return cRed
	case d.Equal(today):
		return cYel
	default:
		return cGreen
	}
}

func DueText(t store.Task) string {
	if t.Due == "" {
		return ""
	}
	if t.Closed() {
		return t.Due
	}
	d, err := store.ParseDate(t.Due)
	if err != nil {
		return t.Due
	}
	today := store.Today()
	if d.Before(today) {
		diff := int(today.Sub(d).Hours() / 24)
		return fmt.Sprintf("%s 逾期 %d 天", t.Due, diff)
	}
	if d.Equal(today) {
		return t.Due + " 今天到期"
	}
	diff := int(d.Sub(today).Hours() / 24)
	if diff <= 3 {
		return fmt.Sprintf("%s 还有 %d 天", t.Due, diff)
	}
	return t.Due
}

func PriorityColor(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "p0":
		return cRed
	case "p1":
		return cRed
	case "p2":
		return cYel
	case "p3":
		return cBlue
	case "p4":
		return cGray
	default:
		return ""
	}
}