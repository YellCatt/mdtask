package report

import (
	"fmt"
	"strings"
	"time"

	"mdtask/internal/store"
)

func BuildDaily(st *store.Store) (subject string, body string, err error) {
	today := store.Today()
	yesterday := today.AddDate(0, 0, -1)
	yesterdayStr := yesterday.Format("2006-01-02")

	archived, err := archivedAll(st)
	if err != nil {
		return "", "", fmt.Errorf("读取归档失败: %w", err)
	}

	doneYesterday := doneInRange(archived, yesterday, yesterday)
	top5, err := openTopN(st, 5)
	if err != nil {
		return "", "", fmt.Errorf("读取待办失败: %w", err)
	}

	subject = fmt.Sprintf("MDTask 日报 · %s 完成 %d 项", yesterdayStr, len(doneYesterday))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 MDTask 日报 — %s\n", yesterdayStr))
	sb.WriteString(strings.Repeat("=", 40))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("✅ 昨日完成 (%d 项)\n", len(doneYesterday)))
	sb.WriteString(strings.Repeat("-", 30))
	writeTasks(&sb, doneYesterday)

	sb.WriteString("\n\n")
	allOpen, _, _ := st.List()
	sb.WriteString(fmt.Sprintf("📋 待办总数: %d  —  最高优先的 %d 项\n", len(allOpen), len(top5)))
	sb.WriteString(strings.Repeat("-", 30))
	if len(allOpen) == 0 {
		sb.WriteString("\n（全部完成，没有待办！🎉）\n")
	} else {
		writeTasks(&sb, top5)
		if len(allOpen)-len(top5) > 0 {
			sb.WriteString(fmt.Sprintf("\n\n  ... 还有 %d 项，优先级较低，暂不列出", len(allOpen)-len(top5)))
		}
	}

	sb.WriteString(fmt.Sprintf("\n\n—— MDTask %s\n", today.Format("2006-01-02 15:04:05")))
	return subject, sb.String(), nil
}

func BuildWeekly(st *store.Store) (subject string, body string, err error) {
	today := store.Today()

	weekday := int(today.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := today.AddDate(0, 0, 1-weekday)
	lastMonday := monday.AddDate(0, 0, -7)
	lastSunday := monday.AddDate(0, 0, -1)

	archived, err := archivedAll(st)
	if err != nil {
		return "", "", fmt.Errorf("读取归档失败: %w", err)
	}
	doneWeek := doneInRange(archived, lastMonday, lastSunday)
	sortDoneByDoneAt(doneWeek)

	top10, err := openTopN(st, 10)
	if err != nil {
		return "", "", fmt.Errorf("读取待办失败: %w", err)
	}

	subject = fmt.Sprintf("MDTask 周报 · %s ~ %s 完成 %d 项",
		lastMonday.Format("2006-01-02"), lastSunday.Format("2006-01-02"), len(doneWeek))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📆 MDTask 周报 — %s ~ %s\n",
		lastMonday.Format("2006-01-02"), lastSunday.Format("2006-01-02")))
	sb.WriteString(strings.Repeat("=", 40))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("✅ 上周完成 (%d 项)\n", len(doneWeek)))
	sb.WriteString(strings.Repeat("-", 30))
	if len(doneWeek) == 0 {
		sb.WriteString("\n（上周没有完成任何任务）\n")
	} else {
		for _, t := range doneWeek {
			sb.WriteString(fmt.Sprintf("\n  [%s] #%s [%s] %s",
				strings.TrimSpace(t.DoneAt), t.ID, strings.TrimSpace(t.Priority), strings.TrimSpace(t.Title)))
			if strings.TrimSpace(t.Note) != "" {
				sb.WriteString(fmt.Sprintf("  — %s", strings.TrimSpace(t.Note)))
			}
		}
	}

	sb.WriteString("\n\n")
	allOpen, _, _ := st.List()
	sb.WriteString(fmt.Sprintf("📋 本周待办总数: %d  —  最高优先的 %d 项\n", len(allOpen), len(top10)))
	sb.WriteString(strings.Repeat("-", 30))
	if len(allOpen) == 0 {
		sb.WriteString("\n（全部完成，没有待办！🎉）\n")
	} else {
		writeTasks(&sb, top10)
		if len(allOpen)-len(top10) > 0 {
			sb.WriteString(fmt.Sprintf("\n\n  ... 还有 %d 项", len(allOpen)-len(top10)))
		}
	}

	sb.WriteString(fmt.Sprintf("\n\n—— MDTask %s\n", today.Format("2006-01-02 15:04:05")))
	return subject, sb.String(), nil
}

func BuildMonthly(st *store.Store) (subject string, body string, err error) {
	today := store.Today()
	firstOfThisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	firstOfLastMonth := firstOfThisMonth.AddDate(0, -1, 0)
	lastOfLastMonth := firstOfThisMonth.AddDate(0, 0, -1)

	archived, err := archivedAll(st)
	if err != nil {
		return "", "", fmt.Errorf("读取归档失败: %w", err)
	}
	doneMonth := doneInRange(archived, firstOfLastMonth, lastOfLastMonth)
	sortDoneByDoneAt(doneMonth)

	top10, err := openTopN(st, 10)
	if err != nil {
		return "", "", fmt.Errorf("读取待办失败: %w", err)
	}

	monthLabel := firstOfLastMonth.Format("2006年01月")
	subject = fmt.Sprintf("MDTask 月报 · %s 完成 %d 项", monthLabel, len(doneMonth))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 MDTask 月报 — %s\n", monthLabel))
	sb.WriteString(strings.Repeat("=", 40))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("✅ 上月完成 (%d 项)\n", len(doneMonth)))
	sb.WriteString(strings.Repeat("-", 30))
	if len(doneMonth) == 0 {
		sb.WriteString("\n（上月没有完成任何任务）\n")
	} else {
		prioCount := map[string]int{}
		for _, t := range doneMonth {
			sb.WriteString(fmt.Sprintf("\n  [%s] #%s [%s] %s",
				strings.TrimSpace(t.DoneAt), t.ID, strings.TrimSpace(t.Priority), strings.TrimSpace(t.Title)))
			if strings.TrimSpace(t.Note) != "" {
				sb.WriteString(fmt.Sprintf("  — %s", strings.TrimSpace(t.Note)))
			}
			prio := strings.ToUpper(strings.TrimSpace(t.Priority))
			prioCount[prio]++
		}
		sb.WriteString("\n\n  完成项按优先级: ")
		first := true
		for _, p := range []string{"P0", "P1", "P2", "P3", "P4"} {
			if c, ok := prioCount[p]; ok {
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s × %d", p, c))
				first = false
			}
		}
	}

	sb.WriteString("\n\n")
	allOpen, _, _ := st.List()
	sb.WriteString(fmt.Sprintf("📋 当前待办总数: %d  —  最高优先的 %d 项\n", len(allOpen), len(top10)))
	sb.WriteString(strings.Repeat("-", 30))
	if len(allOpen) == 0 {
		sb.WriteString("\n（全部完成，没有待办！🎉）\n")
	} else {
		writeTasks(&sb, top10)
		if len(allOpen)-len(top10) > 0 {
			sb.WriteString(fmt.Sprintf("\n\n  ... 还有 %d 项", len(allOpen)-len(top10)))
		}
	}

	sb.WriteString(fmt.Sprintf("\n\n—— MDTask %s\n", today.Format("2006-01-02 15:04:05")))
	return subject, sb.String(), nil
}

func BuildYearly(st *store.Store) (subject string, body string, err error) {
	today := store.Today()
	lastYear := today.Year() - 1
	start := time.Date(lastYear, 1, 1, 0, 0, 0, 0, today.Location())
	end := time.Date(lastYear, 12, 31, 0, 0, 0, 0, today.Location())

	archived, err := archivedAll(st)
	if err != nil {
		return "", "", fmt.Errorf("读取归档失败: %w", err)
	}
	doneYear := doneInRange(archived, start, end)
	sortDoneByDoneAt(doneYear)

	top10, err := openTopN(st, 10)
	if err != nil {
		return "", "", fmt.Errorf("读取待办失败: %w", err)
	}

	subject = fmt.Sprintf("MDTask 年报 · %d 年 完成 %d 项", lastYear, len(doneYear))

	prioCount := map[string]int{}
	monthCount := map[string]int{}
	for _, t := range doneYear {
		prio := strings.ToUpper(strings.TrimSpace(t.Priority))
		prioCount[prio]++
		dt, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(t.DoneAt), today.Location())
		if err == nil {
			monthCount[dt.Format("1月")]++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎯 MDTask 年报 — %d 年\n", lastYear))
	sb.WriteString(strings.Repeat("=", 40))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("✅ 全年完成 (%d 项)\n\n", len(doneYear)))

	if len(doneYear) == 0 {
		sb.WriteString("（全年没有完成任何任务）\n\n")
	} else {
		sb.WriteString("📊 按优先级分布:\n")
		for _, p := range []string{"P0", "P1", "P2", "P3", "P4"} {
			c := prioCount[p]
			if c > 0 {
				sb.WriteString(fmt.Sprintf("  %s: %d 项  %s\n", p, c, strings.Repeat("█", c)))
			}
		}
		sb.WriteString("\n📈 按月分布:\n")
		for m := 1; m <= 12; m++ {
			key := fmt.Sprintf("%d月", m)
			c := monthCount[key]
			if c > 0 {
				sb.WriteString(fmt.Sprintf("  %2d月: %d 项  %s\n", m, c, strings.Repeat("█", c)))
			}
		}
	}

	sb.WriteString("\n\n📋 当前待办总数: ")
	allOpen, _, _ := st.List()
	sb.WriteString(fmt.Sprintf("%d  —  最高优先的 %d 项\n", len(allOpen), len(top10)))
	sb.WriteString(strings.Repeat("-", 30))
	if len(allOpen) == 0 {
		sb.WriteString("\n（全部完成！🎉）\n")
	} else {
		writeTasks(&sb, top10)
		if len(allOpen)-len(top10) > 0 {
			sb.WriteString(fmt.Sprintf("\n\n  ... 还有 %d 项", len(allOpen)-len(top10)))
		}
	}

	sb.WriteString(fmt.Sprintf("\n\n—— MDTask %s\n", today.Format("2006-01-02 15:04:05")))
	return subject, sb.String(), nil
}

func writeTasks(sb *strings.Builder, ts []store.Task) {
	if len(ts) == 0 {
		sb.WriteString("\n（无）")
		return
	}
	for _, t := range ts {
		sb.WriteString(fmt.Sprintf("\n  #%s [%s] %s", t.ID, strings.TrimSpace(t.Priority), strings.TrimSpace(t.Title)))
		if strings.TrimSpace(t.Due) != "" {
			sb.WriteString(fmt.Sprintf("  (截止: %s)", strings.TrimSpace(t.Due)))
		}
		if strings.TrimSpace(t.Note) != "" {
			sb.WriteString(fmt.Sprintf("  — %s", strings.TrimSpace(t.Note)))
		}
	}
}

func sortDoneByDoneAt(ts []store.Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		return strings.TrimSpace(ts[i].DoneAt) < strings.TrimSpace(ts[j].DoneAt)
	})
}