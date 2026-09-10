package app

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/notify"
	"mdtask/internal/store"
)

type periodKind int

const (
	kDaily periodKind = iota
	kWeekly
	kMonthly
	kYearly
)

func parsePeriodKind(s string) (periodKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "d", "day", "daily", "日报", "日":
		return kDaily, true
	case "w", "week", "weekly", "周报", "周":
		return kWeekly, true
	case "m", "month", "monthly", "月报", "月":
		return kMonthly, true
	case "y", "year", "yearly", "annual", "年报", "年":
		return kYearly, true
	}
	return kDaily, false
}

func CmdReport(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	typeStr := fs.String("type", "daily", "报告类型: daily/weekly/monthly/yearly")
	dateStr := fs.String("date", "", "参考日期，默认今天")
	last := fs.Bool("last", false, "统计上一个周期")
	open := fs.Bool("open", cfg.Report.Open, "生成后打开文件")
	noNotify := fs.Bool("no-notify", !cfg.Report.Notify, "不弹通知")
	if err := fs.Parse(args); err != nil {
		return err
	}

	kind, _ := parsePeriodKind(*typeStr)
	ref := time.Now()
	if *dateStr != "" {
		if t, err := time.Parse("2006-01-02", *dateStr); err == nil {
			ref = t
		}
	}

	_ = kind
	_ = ref
	_ = last

	tasks, _, err := st.List()
	if err != nil {
		return err
	}
	arch, _ := st.ListArchive()

	doneCount := 0
	doingCount := 0
	holdCount := 0
	cancelCount := 0
	overdueCount := 0

	for _, t := range tasks {
		switch t.Status {
		case "✅":
			doneCount++
		case "⏸️":
			doingCount++
		case "❌":
			holdCount++
		case "🔴":
			cancelCount++
		default:
		}
		if t.Due != "" && !t.Closed() {
			if d, err := store.ParseDate(t.Due); err == nil && d.Before(store.Today()) {
				overdueCount++
			}
		}
	}

	fmt.Printf("=== MDTask 日报 (%s) ===\n", ref.Format("2006-01-02"))
	fmt.Printf("进行中: %d  停滞: %d  完成: %d  取消: %d  逾期: %d\n",
		doingCount, holdCount, doneCount, cancelCount, overdueCount)
	fmt.Printf("归档任务: %d\n", len(arch))
	fmt.Printf("总任务: %d\n", len(tasks))

	if !*noNotify {
		title := fmt.Sprintf("MDTask %s 报告", *typeStr)
		text := fmt.Sprintf("进行中 %d，停滞 %d，完成 %d，逾期 %d",
			doingCount, holdCount, doneCount, overdueCount)
		notify.Notify(title, text)
	}
	if *open {
		_ = open
	}
	return nil
}