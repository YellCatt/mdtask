package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mdtask/internal/config"
	"mdtask/internal/notify"
	"mdtask/internal/report"
	"mdtask/internal/status"
	"mdtask/internal/store"
)

func CmdReport(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	typeStr := fs.String("type", "daily", "报告类型: daily/weekly/monthly/yearly")
	output := fs.String("o", "", "输出文件路径（可选，默认不写文件）")
	open := fs.Bool("open", cfg.Report.Open, "生成后用默认程序打开报告")
	noNotify := fs.Bool("no-notify", !cfg.Report.Notify, "不弹系统通知")
	if err := fs.Parse(args); err != nil {
		return err
	}

	kind, ok := parsePeriodKind(*typeStr)
	if !ok {
		fmt.Fprintf(os.Stderr, "未知的报告类型: %s，支持 daily/weekly/monthly/yearly\n", *typeStr)
		return fmt.Errorf("未知的报告类型: %s", *typeStr)
	}

	tasks, _, err := st.List()
	if err != nil {
		return err
	}
	arch, _ := st.ListArchive()

	var subject, body string
	switch kind {
	case kDaily:
		subject, body, _ = report.BuildDaily(arch, tasks)
	case kWeekly:
		subject, body, _ = report.BuildWeekly(arch, tasks)
	case kMonthly:
		subject, body, _ = report.BuildMonthly(arch, tasks)
	case kYearly:
		subject, body, _ = report.BuildYearly(arch, tasks)
	}

	fmt.Print(subject, "\n\n", body, "\n")

	if *output != "" {
		out := *output
		if !filepath.IsAbs(out) {
			out = filepath.Join(st.Dir, out)
		}
		if err := os.WriteFile(out, []byte(subject+"\n\n"+body), 0o644); err != nil {
			return fmt.Errorf("写入报告文件失败: %w", err)
		}
		fmt.Fprintf(os.Stderr, "报告已写入 %s\n", out)

		if *open {
			if err := notify.OpenFile(out); err != nil {
				fmt.Fprintf(os.Stderr, "打开报告文件失败: %v\n", err)
			}
		}
	}

	if !*noNotify {
		title := subject
		var sb strings.Builder
		doneCount := 0
		doingCount := 0
		overdueCount := 0
		for _, t := range tasks {
			switch {
			case t.Closed():
				doneCount++
			case status.IsClosed(t.Status):
			default:
				doingCount++
			}
			if t.Due != "" && !t.Closed() {
				if d, err := store.ParseDate(t.Due); err == nil && d.Before(store.Today()) {
					overdueCount++
				}
			}
		}
		sb.WriteString(fmt.Sprintf("进行中 %d，已完成 %d，逾期 %d，归档 %d", doingCount, doneCount, overdueCount, len(arch)))
		notify.Notify(title, sb.String())
	}

	return nil
}