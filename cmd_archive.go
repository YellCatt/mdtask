package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// autoArchiveDays <0 表示关闭；0 = 结束态立刻归档；>0 = 截止日期在 N 天前才归档
var autoArchiveDays = -1

// initAutoArchive 配置里的 archive.auto 为准，环境变量可以临时覆盖
func initAutoArchive() {
	autoArchiveDays = cfg.Archive.Auto
	v := strings.TrimSpace(os.Getenv("MDTASK_AUTO_ARCHIVE"))
	if v == "" {
		return
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		autoArchiveDays = n
		return
	}
	autoArchiveDays = 0
}

// tryAutoArchive 每次改完状态后跑一次，把到期的结束态任务搬进归档
func tryAutoArchive() {
	if autoArchiveDays < 0 {
		return
	}
	cutoff := time.Now()
	if autoArchiveDays > 0 {
		cutoff = cutoff.AddDate(0, 0, -autoArchiveDays)
	}
	pred := func(t Task) bool {
		if !isClosedStatus(t.Status) {
			return false
		}
		if autoArchiveDays == 0 {
			return true
		}
		due, err := time.Parse("2006-01-02", strings.TrimSpace(t.Due))
		if err != nil { // 没写截止日期的先不动
			return false
		}
		return due.Before(cutoff)
	}
	var moved int
	if err := st.Update(func(s *Store) error {
		moved = s.Archive(pred)
		return nil
	}); err != nil || moved == 0 {
		return
	}
	fmt.Println(paint(cGray, fmt.Sprintf("（自动归档 %d 项到「## %s」）", moved, st.archTitle)))
}

func cmdArchive(args []string) {
	fs := flag.NewFlagSet("archive", flag.ExitOnError)
	before := fs.String("before", "", "只归档截止日期早于该日期的（YYYY-MM-DD）")
	days := fs.Int("days", 0, "只归档截止日期在 N 天之前的")
	all := fs.Bool("all", cfg.Archive.IncludeStuck, "连 ❌ 停滞 也一起归档")
	fs.Parse(args)

	var cutoff time.Time
	hasCutoff := false
	switch {
	case *before != "":
		d, err := time.Parse("2006-01-02", strings.TrimSpace(*before))
		if err != nil {
			fatal(fmt.Errorf("-before 需要 YYYY-MM-DD 格式: %w", err))
		}
		cutoff, hasCutoff = d, true
	case *days > 0:
		cutoff, hasCutoff = time.Now().AddDate(0, 0, -*days), true
	}

	pred := func(t Task) bool {
		d := statusDefOf(t.Status)
		if d == nil { // 空状态是待办，不归档
			return false
		}
		if !d.Closed && !(*all && d.Key == stHold) {
			return false
		}
		if !hasCutoff {
			return true
		}
		due, err := time.Parse("2006-01-02", strings.TrimSpace(t.Due))
		if err != nil { // 没写截止日期的先不动
			return false
		}
		return due.Before(cutoff)
	}

	var moved int
	if err := st.Update(func(s *Store) error {
		moved = s.Archive(pred)
		return nil
	}); err != nil {
		fatal(err)
	}
	if moved == 0 {
		fmt.Println(paint(cGray, "没有需要归档的任务"))
		return
	}
	fmt.Printf("已归档 %d 项到「## %s」\n", moved, st.archTitle)
}
