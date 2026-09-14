// Package util 提供日期解析、星期计算、今日零点以及优先级排序等通用辅助函数。
package util

import (
	"errors"
	"strings"
	"time"
)

// ErrNoDate 表示字符串无法解析为有效日期。
var ErrNoDate = errors.New("no date")

// Today 返回当地时区下「今天 00:00:00」，去掉时分秒便于按天比较。
func Today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// MondayOf 返回 t 所在周的周一（以周一为一周起点），时间归零到 0 点。
func MondayOf(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t.AddDate(0, 0, 1-wd)
}

// PriorityRank 返回优先级权重，数值越大越紧急（P0=5，P4=1，未知=0）。
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

// ParseDate 按多种常见格式解析日期字符串（支持「月-日」自动补当年年份），
// 返回当地时区时间；空串或不合法返回 ErrNoDate。
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