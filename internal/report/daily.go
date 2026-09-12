package report

import (
	"fmt"
	"strings"
	"time"

	"mdtask/internal/logger"
	"mdtask/internal/store"
	"mdtask/internal/util"
)

func BuildDaily(archived, open []store.Task) (subject string, body string, err error) {
	logger.Debug("BuildDaily 开始", "archived", len(archived), "open", len(open))
	today := util.Today()
	yesterday := today.AddDate(0, 0, -1)
	yesterdayStr := yesterday.Format("2006-01-02")

	doneYesterday := doneInRange(archived, yesterday, yesterday)
	top5 := topByPrio(open, 5)

	subject = fmt.Sprintf("MDTask 日报 · %s 完成 %d 项", yesterdayStr, len(doneYesterday))

	var sb strings.Builder
	writeHeader(&sb, "📅", fmt.Sprintf("MDTask 日报 — %s", yesterdayStr))
	writeDoneSection(&sb, "昨日完成", doneYesterday, "（昨日没有完成任何任务）", false)
	writeOpenSection(&sb, "待办总数", open, top5)
	writeFooter(&sb, today)
	return subject, sb.String(), nil
}

func BuildWeekly(archived, open []store.Task) (subject string, body string, err error) {
	logger.Debug("BuildWeekly 开始", "archived", len(archived), "open", len(open))
	today := util.Today()

	monday := util.MondayOf(today)
	lastMonday := monday.AddDate(0, 0, -7)
	lastSunday := monday.AddDate(0, 0, -1)

	doneWeek := doneInRange(archived, lastMonday, lastSunday)
	sortDoneByDoneAt(doneWeek)
	top10 := topByPrio(open, 10)

	subject = fmt.Sprintf("MDTask 周报 · %s ~ %s 完成 %d 项",
		lastMonday.Format("2006-01-02"), lastSunday.Format("2006-01-02"), len(doneWeek))

	var sb strings.Builder
	writeHeader(&sb, "📆", fmt.Sprintf("MDTask 周报 — %s ~ %s",
		lastMonday.Format("2006-01-02"), lastSunday.Format("2006-01-02")))
	writeDoneSection(&sb, "上周完成", doneWeek, "（上周没有完成任何任务）", true)
	writeOpenSection(&sb, "本周待办总数", open, top10)
	writeFooter(&sb, today)
	return subject, sb.String(), nil
}

func BuildMonthly(archived, open []store.Task) (subject string, body string, err error) {
	logger.Debug("BuildMonthly 开始", "archived", len(archived), "open", len(open))
	today := util.Today()
	firstOfThisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	firstOfLastMonth := firstOfThisMonth.AddDate(0, -1, 0)
	lastOfLastMonth := firstOfThisMonth.AddDate(0, 0, -1)

	doneMonth := doneInRange(archived, firstOfLastMonth, lastOfLastMonth)
	sortDoneByDoneAt(doneMonth)
	top10 := topByPrio(open, 10)

	monthLabel := firstOfLastMonth.Format("2006年01月")
	subject = fmt.Sprintf("MDTask 月报 · %s 完成 %d 项", monthLabel, len(doneMonth))

	var sb strings.Builder
	writeHeader(&sb, "📊", fmt.Sprintf("MDTask 月报 — %s", monthLabel))
	writeDoneSection(&sb, "上月完成", doneMonth, "（上月没有完成任何任务）", true)

	if len(doneMonth) > 0 {
		prioCount := prioCountMap(doneMonth)
		sb.WriteString("\n\n  完成项按优先级: ")
		first := true
		for _, p := range prioOrder {
			if c, ok := prioCount[p]; ok {
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s × %d", p, c))
				first = false
			}
		}
	}

	writeOpenSection(&sb, "当前待办总数", open, top10)
	writeFooter(&sb, today)
	return subject, sb.String(), nil
}

func BuildYearly(archived, open []store.Task) (subject string, body string, err error) {
	logger.Debug("BuildYearly 开始", "archived", len(archived), "open", len(open))
	today := util.Today()
	lastYear := today.Year() - 1
	start := time.Date(lastYear, 1, 1, 0, 0, 0, 0, today.Location())
	end := time.Date(lastYear, 12, 31, 0, 0, 0, 0, today.Location())

	doneYear := doneInRange(archived, start, end)
	sortDoneByDoneAt(doneYear)
	top10 := topByPrio(open, 10)

	subject = fmt.Sprintf("MDTask 年报 · %d 年 完成 %d 项", lastYear, len(doneYear))

	prioCount := prioCountMap(doneYear)
	monthCount := map[string]int{}
	for _, t := range doneYear {
		dt, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(t.DoneAt), today.Location())
		if err == nil {
			monthCount[dt.Format("1月")]++
		}
	}

	var sb strings.Builder
	writeHeader(&sb, "🎯", fmt.Sprintf("MDTask 年报 — %d 年", lastYear))

	sb.WriteString(fmt.Sprintf("✅ 全年完成 (%d 项)\n\n", len(doneYear)))
	if len(doneYear) == 0 {
		sb.WriteString("（全年没有完成任何任务）\n")
	} else {
		sb.WriteString("📊 按优先级分布:\n")
		for _, p := range prioOrder {
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

	writeOpenSection(&sb, "当前待办总数", open, top10)
	writeFooter(&sb, today)
	return subject, sb.String(), nil
}