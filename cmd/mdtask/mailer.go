package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/logger"
	"mdtask/internal/mail"
	"mdtask/internal/report"
	"mdtask/internal/store"
)

type sentTracker struct {
	dailyKey   string
	weeklyKey  string
	monthlyKey string
	yearlyKey  string
}

func dumpAllReports(st *store.Store, cfg *config.Config, baseDir string) {
	logger.Info("dumpAllReports: 启动时生成四份报告")
	today := store.Today()
	root := strings.TrimSpace(cfg.Report.Dir)
	if root == "" {
		root = "reports"
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(baseDir, root)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		logger.Error("创建报告根目录失败", "dir", root, "err", err)
		return
	}

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
	monday := store.MondayOf(t)
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
		return store.MondayOf(now).Format("2006-01-02")
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

	monday := store.MondayOf(t)
	st.weeklyKey = monday.Format("2006-01-02")
	if cfg.Report.Weekly > 0 {
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
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

type clock struct{ h, m int }

// reportClocks 解析配置里所有触发时间点，去重并按时间先后排序。
func reportClocks(cfg *config.Config) []clock {
	var out []clock
	seen := map[clock]bool{}
	for _, s := range cfg.Report.Times {
		parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
		if len(parts) != 2 {
			continue
		}
		h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			continue
		}
		c := clock{h, m}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].h*60+out[i].m < out[j].h*60+out[j].m })
	return out
}

// reportHourMin 返回当天最早的时间点，用于判断“今天的报告是否已过触发时刻”。
func reportHourMin(cfg *config.Config) (int, int) {
	if cs := reportClocks(cfg); len(cs) > 0 {
		return cs[0].h, cs[0].m
	}
	return 5, 0
}

// nextReportTime 返回所有配置时间点中，严格晚于 from 的最近一个时刻。
func nextReportTime(cfg *config.Config, from time.Time) time.Time {
	var best time.Time
	for _, c := range reportClocks(cfg) {
		t := time.Date(from.Year(), from.Month(), from.Day(), c.h, c.m, 0, 0, from.Location())
		if !t.After(from) {
			t = t.AddDate(0, 0, 1)
		}
		if best.IsZero() || t.Before(best) {
			best = t
		}
	}
	if best.IsZero() {
		best = from.Add(24 * time.Hour)
	}
	return best
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
		freq  string
		check func(currKey string) bool
		build func() (string, string, error)
		mark  func(currKey string)
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