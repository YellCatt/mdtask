package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/logger"
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

	if err := logger.Init(abs); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 日志初始化失败: %v\n", err)
	}

	logger.Info("MDTask 启动",
		"dir", abs,
		"config_path", configPath,
		"backup", !noBackup && cfg.Backup,
	)
	logger.Debug("当前配置摘要",
		"report_times", cfg.Report.Times,
		"report_interval", cfg.Report.Interval,
		"weekly", cfg.Report.Weekly,
		"monthly", cfg.Report.Monthly,
		"yearly", cfg.Report.Yearly,
		"mail_host", cfg.Mail.SMTPHost,
		"mail_port", cfg.Mail.SMTPPort,
		"mail_from", cfg.Mail.FromEmail,
	)

	ui.InitColor(cfg.Color)
	st := store.NewStore(abs, cfg.Backup && !noBackup)

	known, err := st.CollectKnownIDs()
	if err != nil {
		logger.Error("初始化 CollectKnownIDs 失败", "err", err)
		fatal(fmt.Errorf("初始化失败: %w", err))
	}
	logger.Info("初始化完成", "known_tasks", len(known))

	dumpAllReports(st, abs)

	logger.Info("启动邮件调度 goroutine")
	go runMailer(st, cfg)

	logger.Info("进入主循环: 每 10s 扫描 tasks 目录")
	for {
		time.Sleep(10 * time.Second)
		known, err = st.TouchAddedDates(known)
		if err != nil {
			logger.Error("TouchAddedDates 扫描出错", "err", err)
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
	logger.Info("dumpAllReports: 启动时生成四份报告")
	today := store.Today()
	root := filepath.Join(baseDir, "reports")

	archived, _ := st.ListArchive()
	open, _, _ := st.List()
	logger.Debug("dumpAllReports 读取数据完成",
		"open", len(open),
		"archived", len(archived),
	)

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
			logger.Error("创建报告目录失败", "dir", fullDir, "err", err)
			continue
		}
		fullPath := filepath.Join(fullDir, r.filename)
		if r.err != nil {
			logger.Error("生成报告失败", "freq", r.dir, "err", r.err)
			continue
		}
		content := r.subject + "\n\n" + r.body
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			logger.Error("写入报告文件失败", "path", fullPath, "err", err)
		} else {
			logger.Info("报告已生成", "path", fullPath)
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
	logger.Debug("initSentTracker 初始化完成",
		"dailyKey", st.dailyKey,
		"weeklyKey", st.weeklyKey,
		"monthlyKey", st.monthlyKey,
		"yearlyKey", st.yearlyKey,
	)
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
		wait := next.Sub(now)
		logger.Info("runMailer: 计算下次发送时间",
			"next", next.Format(time.RFC3339),
			"wait_seconds", int(wait.Seconds()),
		)
		time.Sleep(wait)

		now = time.Now()
		logger.Info("runMailer: 到达触发时间，开始发送报告", "now", now.Format(time.RFC3339))
		sendAllReports(st, cfg, tracker, now)
	}
}

func sendAllReports(st *store.Store, cfg *config.Config, tracker *sentTracker, now time.Time) {
	logger.Debug("sendAllReports 开始", "now", now.Format(time.RFC3339))
	archived, _ := st.ListArchive()
	open, _, _ := st.List()
	logger.Debug("sendAllReports 读取数据完成",
		"open", len(open),
		"archived", len(archived),
	)

	tasks := []struct {
		freq   string
		check  func(currKey string) bool
		build  func() (string, string, error)
		mark   func(currKey string)
	}{
		{
			"daily",
			func(k string) bool {
				triggered := k != tracker.dailyKey
				logger.Debug("daily 检查", "currKey", k, "tracker", tracker.dailyKey, "triggered", triggered)
				return triggered
			},
			func() (string, string, error) { return report.BuildDaily(archived, open) },
			func(k string) { tracker.dailyKey = k },
		},
		{
			"weekly",
			func(k string) bool {
				if cfg.Report.Weekly == 0 {
					logger.Debug("weekly 关闭 (cfg.Report.Weekly=0)")
					return false
				}
				wd := int(now.Weekday())
				if wd == 0 {
					wd = 7
				}
				if wd != cfg.Report.Weekly {
					logger.Debug("weekly 跳过，不是指定星期几", "today_wd", wd, "target", cfg.Report.Weekly)
					return false
				}
				triggered := k != tracker.weeklyKey
				logger.Debug("weekly 检查", "currKey", k, "tracker", tracker.weeklyKey, "triggered", triggered)
				return triggered
			},
			func() (string, string, error) { return report.BuildWeekly(archived, open) },
			func(k string) { tracker.weeklyKey = k },
		},
		{
			"monthly",
			func(k string) bool {
				if cfg.Report.Monthly == 0 {
					logger.Debug("monthly 关闭 (cfg.Report.Monthly=0)")
					return false
				}
				if now.Day() != cfg.Report.Monthly {
					logger.Debug("monthly 跳过，不是指定日期", "today_day", now.Day(), "target", cfg.Report.Monthly)
					return false
				}
				triggered := k != tracker.monthlyKey
				logger.Debug("monthly 检查", "currKey", k, "tracker", tracker.monthlyKey, "triggered", triggered)
				return triggered
			},
			func() (string, string, error) { return report.BuildMonthly(archived, open) },
			func(k string) { tracker.monthlyKey = k },
		},
		{
			"yearly",
			func(k string) bool {
				if cfg.Report.Yearly == "" {
					logger.Debug("yearly 关闭 (cfg.Report.Yearly 为空)")
					return false
				}
				parts := strings.SplitN(cfg.Report.Yearly, "-", 2)
				if len(parts) != 2 {
					logger.Debug("yearly 跳过，cfg 格式错误", "yearly", cfg.Report.Yearly)
					return false
				}
				mt, _ := time.Parse("01", parts[0])
				d, _ := time.Parse("02", parts[1])
				if int(now.Month()) != int(mt.Month()) || now.Day() != d.Day() {
					logger.Debug("yearly 跳过，不是指定日期", "today", now.Format("01-02"), "target", cfg.Report.Yearly)
					return false
				}
				triggered := k != tracker.yearlyKey
				logger.Debug("yearly 检查", "currKey", k, "tracker", tracker.yearlyKey, "triggered", triggered)
				return triggered
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
			logger.Error("报告生成失败", "freq", t.freq, "err", err)
			continue
		}
		logger.Debug("报告生成成功", "freq", t.freq, "subject", subject)
		if err := mail.Send(cfg, subject, body); err != nil {
			logger.Error("邮件发送失败", "freq", t.freq, "err", err)
		} else {
			logger.Info("报告邮件已发送", "freq", t.freq, "subject", subject)
			t.mark(currKey)
		}
	}
}

func fatal(v any) {
	logger.Error("fatal 退出", "err", v)
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