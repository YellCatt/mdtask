package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/mail"
	"mdtask/internal/report"
	"mdtask/internal/store"
	"mdtask/internal/ui"
)

func main() {
	file, configPath, noBackup, _ := splitArgs(os.Args[1:])

	cfg, _, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	if file == "" {
		file = os.Getenv("MDTASK_FILE")
	}
	if file == "" {
		file = cfg.Dir
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		fatal(err)
	}

	ui.InitColor(cfg.Color)
	st := store.NewStore(abs, cfg.Backup && !noBackup)

	known, err := st.CollectKnownIDs()
	if err != nil {
		fatal(fmt.Errorf("初始化失败: %w", err))
	}

	dumpAllReports(st, abs)

	go runMailer(st, cfg)

	for {
		time.Sleep(10 * time.Second)
		known, err = st.TouchAddedDates(known)
		if err != nil {
			fmt.Fprintln(os.Stderr, "扫描出错:", err)
		}
	}
}

type sentTracker struct {
	dailyKey   string
	weeklyKey  string
	monthlyKey string
	yearlyKey  string
}

func dumpAllReports(st *store.Store, baseDir string) {
	today := store.Today()
	root := filepath.Join(baseDir, "reports")

	archived, _ := st.ListArchive()
	open, _, _ := st.List()

	type entry struct {
		dir      string
		filename string
		subject  string
		body     string
		err      error
	}

	results := []entry{}

	subject, body, err := report.BuildDaily(archived, open)
	results = append(results, entry{"daily", today.AddDate(0, 0, -1).Format("2006-01-02") + ".txt", subject, body, err})

	subject, body, err = report.BuildWeekly(archived, open)
	results = append(results, entry{"week", weekLabel(today) + ".txt", subject, body, err})

	subject, body, err = report.BuildMonthly(archived, open)
	results = append(results, entry{"month", monthLabel(today) + ".txt", subject, body, err})

	subject, body, err = report.BuildYearly(archived, open)
	results = append(results, entry{"year", yearLabel(today) + ".txt", subject, body, err})

	for _, r := range results {
		fullDir := filepath.Join(root, r.dir)
		if err := os.MkdirAll(fullDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "创建报告目录 %s 失败: %v\n", fullDir, err)
			continue
		}
		fullPath := filepath.Join(fullDir, r.filename)
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "生成 %s 报告失败: %v\n", r.dir, r.err)
			continue
		}
		content := r.subject + "\n\n" + r.body
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写入报告 %s 失败: %v\n", fullPath, err)
		} else {
			fmt.Fprintf(os.Stderr, "报告已生成: %s\n", fullPath)
		}
	}
}

func weekLabel(t time.Time) string {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := t.AddDate(0, 0, 1-wd)
	sunday := monday.AddDate(0, 0, 6)
	return monday.Format("2006-01-02") + "_" + sunday.Format("2006-01-02")
}

func monthLabel(t time.Time) string {
	firstOfThisMonth := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	lastOfLastMonth := firstOfThisMonth.AddDate(0, 0, -1)
	return lastOfLastMonth.Format("2006-01")
}

func yearLabel(t time.Time) string {
	return fmt.Sprintf("%d", t.Year()-1)
}

func periodKey(now time.Time, freq string, cfg *config.Config) string {
	switch freq {
	case "daily":
		return now.Format("2006-01-02")
	case "weekly":
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := now.AddDate(0, 0, 1-wd)
		return monday.Format("2006-01-02")
	case "monthly":
		return now.Format("2006-01")
	case "yearly":
		return now.Format("2006")
	}
	return ""
}

func initSentTracker(cfg *config.Config, t time.Time) *sentTracker {
	st := &sentTracker{}
	hour, min := reportHourMin(cfg)
	todayTriggered := false
	trigger := time.Date(t.Year(), t.Month(), t.Day(), hour, min, 0, 0, t.Location())
	if !t.Before(trigger) {
		todayTriggered = true
	}

	st.dailyKey = t.Format("2006-01-02")
	if !todayTriggered {
		st.dailyKey = t.AddDate(0, 0, -1).Format("2006-01-02")
	}

	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := t.AddDate(0, 0, 1-wd)
	st.weeklyKey = monday.Format("2006-01-02")
	if cfg.Report.Weekly > 0 {
		targetMonWeekday := cfg.Report.Weekly
		if wd > targetMonWeekday || (wd == targetMonWeekday && todayTriggered) {
			st.weeklyKey = monday.Format("2006-01-02")
		} else {
			st.weeklyKey = monday.AddDate(0, 0, -7).Format("2006-01-02")
		}
	}

	if cfg.Report.Monthly > 0 {
		if t.Day() > cfg.Report.Monthly || (t.Day() == cfg.Report.Monthly && todayTriggered) {
			st.monthlyKey = t.Format("2006-01")
		} else {
			prev := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, -1, 0)
			st.monthlyKey = prev.Format("2006-01")
		}
	}

	if cfg.Report.Yearly != "" {
		parts := strings.SplitN(cfg.Report.Yearly, "-", 2)
		if len(parts) == 2 {
			mt, _ := time.Parse("01", parts[0])
			d, _ := time.Parse("02", parts[1])
			triggerDate := time.Date(t.Year(), mt.Month(), d.Day(), hour, min, 0, 0, t.Location())
			if !t.Before(triggerDate) {
				st.yearlyKey = t.Format("2006")
			} else {
				st.yearlyKey = t.AddDate(-1, 0, 0).Format("2006")
			}
		}
	}
	return st
}

func reportHourMin(cfg *config.Config) (int, int) {
	times := cfg.Report.Times
	if len(times) == 0 {
		return 5, 0
	}
	last := times[len(times)-1]
	parts := strings.SplitN(last, ":", 2)
	h := 5
	m := 0
	if len(parts) == 2 {
		fmt.Sscanf(parts[0], "%d", &h)
		fmt.Sscanf(parts[1], "%d", &m)
	} else if len(parts) == 1 {
		fmt.Sscanf(parts[0], "%d", &h)
	}
	return h, m
}

func nextReportTime(cfg *config.Config, from time.Time) time.Time {
	h, m := reportHourMin(cfg)
	t := time.Date(from.Year(), from.Month(), from.Day(), h, m, 0, 0, from.Location())
	if !t.After(from) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func runMailer(st *store.Store, cfg *config.Config) {
	tracker := initSentTracker(cfg, time.Now())
	for {
		now := time.Now()
		next := nextReportTime(cfg, now)
		time.Sleep(next.Sub(now))

		now = time.Now()
		sendAllReports(st, cfg, tracker, now)
	}
}

func sendAllReports(st *store.Store, cfg *config.Config, tracker *sentTracker, now time.Time) {
	archived, _ := st.ListArchive()
	open, _, _ := st.List()

	tasks := []struct {
		freq   string
		check  func(currKey string) bool
		build  func() (string, string, error)
		mark   func(currKey string)
	}{
		{
			"daily",
			func(k string) bool { return k != tracker.dailyKey },
			func() (string, string, error) { return report.BuildDaily(archived, open) },
			func(k string) { tracker.dailyKey = k },
		},
		{
			"weekly",
			func(k string) bool {
				if cfg.Report.Weekly == 0 {
					return false
				}
				wd := int(now.Weekday())
				if wd == 0 {
					wd = 7
				}
				if wd != cfg.Report.Weekly {
					return false
				}
				return k != tracker.weeklyKey
			},
			func() (string, string, error) { return report.BuildWeekly(archived, open) },
			func(k string) { tracker.weeklyKey = k },
		},
		{
			"monthly",
			func(k string) bool {
				if cfg.Report.Monthly == 0 {
					return false
				}
				if now.Day() != cfg.Report.Monthly {
					return false
				}
				return k != tracker.monthlyKey
			},
			func() (string, string, error) { return report.BuildMonthly(archived, open) },
			func(k string) { tracker.monthlyKey = k },
		},
		{
			"yearly",
			func(k string) bool {
				if cfg.Report.Yearly == "" {
					return false
				}
				parts := strings.SplitN(cfg.Report.Yearly, "-", 2)
				if len(parts) != 2 {
					return false
				}
				mt, _ := time.Parse("01", parts[0])
				d, _ := time.Parse("02", parts[1])
				if int(now.Month()) != int(mt.Month()) || now.Day() != d.Day() {
					return false
				}
				return k != tracker.yearlyKey
			},
			func() (string, string, error) { return report.BuildYearly(archived, open) },
			func(k string) { tracker.yearlyKey = k },
		},
	}

	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].freq < tasks[j].freq })

	for _, t := range tasks {
		currKey := periodKey(now, t.freq, cfg)
		if !t.check(currKey) {
			continue
		}
		subject, body, err := t.build()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s [%s] 生成失败: %v\n", time.Now().Format(time.RFC3339), t.freq, err)
			continue
		}
		if err := mail.Send(cfg, subject, body); err != nil {
			fmt.Fprintf(os.Stderr, "%s [%s] 发送失败: %v\n", time.Now().Format(time.RFC3339), t.freq, err)
		} else {
			fmt.Fprintf(os.Stderr, "%s [%s] 已发送\n", time.Now().Format(time.RFC3339), t.freq)
			t.mark(currKey)
		}
	}
}

func fatal(v any) {
	fmt.Fprintln(os.Stderr, "错误:", v)
	os.Exit(1)
}

func splitArgs(args []string) (file, config string, noBackup bool, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-file" || a == "--file" || a == "-f":
			if i+1 < len(args) {
				file = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-file="):
			file = strings.TrimPrefix(a, "-file=")
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case strings.HasPrefix(a, "-f="):
			file = strings.TrimPrefix(a, "-f=")
		case a == "-config" || a == "--config" || a == "-c":
			if i+1 < len(args) {
				config = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-config="):
			config = strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--config="):
			config = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			config = strings.TrimPrefix(a, "-c=")
		case a == "-no-backup" || a == "--no-backup":
			noBackup = true
		default:
			rest = append(rest, a)
		}
	}
	return
}