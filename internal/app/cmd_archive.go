package app

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/status"
	"mdtask/internal/store"
)

func getAutoArchiveDays(cfg *config.Config) int {
	days := cfg.Archive.Auto
	if v := strings.TrimSpace(os.Getenv("MDTASK_AUTO_ARCHIVE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			days = n
		} else {
			days = 0
		}
	}
	return days
}

func tryAutoArchive(st *store.Store, cfg *config.Config) {
	days := getAutoArchiveDays(cfg)
	if days < 0 {
		return
	}
	cutoff := time.Now()
	if days > 0 {
		cutoff = cutoff.AddDate(0, 0, -days)
	}
	pred := func(t store.Task) bool {
		if !status.IsClosed(t.Status) {
			return false
		}
		if days == 0 {
			return true
		}
		due, err := time.Parse("2006-01-02", strings.TrimSpace(t.Due))
		if err != nil {
			return false
		}
		return due.Before(cutoff)
	}
	var moved int
	if err := st.Update(func(s *store.Store) error {
		moved = s.Archive(pred)
		return nil
	}); err != nil || moved == 0 {
		return
	}
	fmt.Printf("（自动归档 %d 项）\n", moved)
}

func CmdArchive(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("archive", flag.ExitOnError)
	before := fs.String("before", "", "只归档截止日期早于该日期的（YYYY-MM-DD）")
	days := fs.Int("days", 0, "只归档截止日期在 N 天之前的")
	all := fs.Bool("all", cfg.Archive.IncludeStuck, "连 ❌ 停滞 也一起归档")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var cutoff time.Time
	hasCutoff := false
	switch {
	case *before != "":
		d, err := time.Parse("2006-01-02", strings.TrimSpace(*before))
		if err != nil {
			return fmt.Errorf("-before 需要 YYYY-MM-DD 格式: %w", err)
		}
		cutoff, hasCutoff = d, true
	case *days > 0:
		cutoff, hasCutoff = time.Now().AddDate(0, 0, -*days), true
	}

	pred := func(t store.Task) bool {
		d := status.DefOf(t.Status)
		if d == nil {
			return false
		}
		if !d.Closed && !(*all && d.Key == status.Hold) {
			return false
		}
		if !hasCutoff {
			return true
		}
		due, err := time.Parse("2006-01-02", strings.TrimSpace(t.Due))
		if err != nil {
			return false
		}
		return due.Before(cutoff)
	}

	var moved int
	if err := st.Update(func(s *store.Store) error {
		moved = s.Archive(pred)
		return nil
	}); err != nil {
		return err
	}
	if moved == 0 {
		fmt.Println("没有需要归档的任务")
		return nil
	}
	fmt.Printf("已归档 %d 项\n", moved)
	return nil
}