// Package report 负责根据归档/待办任务生成日报、周报、月报、年报的纯文本正文。
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"mdtask/internal/store"
	"mdtask/internal/util"
)

const (
	sepHead   = "========================================"
	sepSub    = "------------------------------"
	footerFmt = "\n\n—— MDTask %s\n"
)

var prioOrder = []string{"P0", "P1", "P2", "P3", "P4"}

// sortByPrio 按优先级权重降序排序（权重相同按 ID 升序），不改变原切片。
func sortByPrio(ts []store.Task) []store.Task {
	out := append([]store.Task(nil), ts...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := util.PriorityRank(out[i].Priority), util.PriorityRank(out[j].Priority)
		if ri != rj {
			return ri > rj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// sortDoneByDoneAt 按完成时间升序排序（用于周/月/年报中按时间列出已完成项）。
func sortDoneByDoneAt(ts []store.Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		return strings.TrimSpace(ts[i].DoneAt) < strings.TrimSpace(ts[j].DoneAt)
	})
}

// doneInRange 从归档任务里挑出「完成时间落在 [from,to] 闭区间」的任务（按字符串日期比较）。
func doneInRange(archived []store.Task, from, to time.Time) []store.Task {
	fromS := from.Format("2006-01-02")
	toS := to.Format("2006-01-02")
	var hit []store.Task
	for _, t := range archived {
		d := strings.TrimSpace(t.DoneAt)
		if d == "" {
			continue
		}
		if d >= fromS && d <= toS {
			hit = append(hit, t)
		}
	}
	return hit
}

// prioCountMap 统计各优先级（P0~P4）的任务数量。
func prioCountMap(tasks []store.Task) map[string]int {
	m := map[string]int{}
	for _, t := range tasks {
		p := strings.ToUpper(strings.TrimSpace(t.Priority))
		m[p]++
	}
	return m
}

// topByPrio 返回待办里优先级最高的前 n 项；n <= 0 表示全部返回。
func topByPrio(open []store.Task, n int) []store.Task {
	sorted := sortByPrio(open)
	if n <= 0 || len(sorted) < n {
		return sorted
	}
	return sorted[:n]
}

// writeHeader 写报告大标题与分隔线。
func writeHeader(sb *strings.Builder, emoji, title string) {
	sb.WriteString(fmt.Sprintf("%s %s\n", emoji, title))
	sb.WriteString(sepHead)
	sb.WriteString("\n\n")
}

// taskLine 返回任务的展示文本：#ID [优先级] 标题（优先级为空时不显示）。
func taskLine(t store.Task) string {
	p := strings.TrimSpace(t.Priority)
	if p != "" {
		return fmt.Sprintf("#%s [%s] %s", t.ID, p, strings.TrimSpace(t.Title))
	}
	return fmt.Sprintf("#%s %s", t.ID, strings.TrimSpace(t.Title))
}

// writeDoneSection 写「已完成」区块；showDoneAt 为 true 时每行带完成时间，空则显示提示语。
func writeDoneSection(sb *strings.Builder, label string, tasks []store.Task, emptyMsg string, showDoneAt bool) {
	sb.WriteString(fmt.Sprintf("✅ %s (%d 项)\n", label, len(tasks)))
	sb.WriteString(sepSub)
	if len(tasks) == 0 {
		sb.WriteString(fmt.Sprintf("\n%s\n", emptyMsg))
		return
	}
	for _, t := range tasks {
		if showDoneAt {
			sb.WriteString(fmt.Sprintf("\n  [%s]", strings.TrimSpace(t.DoneAt)))
		} else {
			sb.WriteString("\n ")
		}
		sb.WriteString(" " + taskLine(t))
	}
}

// writeOpenSection 写「待办」区块；topN 由调用方决定——如果 topN 等于 allOpen，则不显示「还有 N 项」的提示。
func writeOpenSection(sb *strings.Builder, label string, allOpen []store.Task, topN []store.Task) {
	sb.WriteString("\n\n")
	if len(topN) == len(allOpen) {
		sb.WriteString(fmt.Sprintf("📋 %s: %d  —  全部列出\n", label, len(allOpen)))
	} else {
		sb.WriteString(fmt.Sprintf("📋 %s: %d  —  最高优先的 %d 项\n", label, len(allOpen), len(topN)))
	}
	sb.WriteString(sepSub)
	if len(allOpen) == 0 {
		sb.WriteString("\n（全部完成，没有待办！🎉）\n")
		return
	}
	for _, t := range topN {
		sb.WriteString("\n  " + taskLine(t))
	}
	remaining := len(allOpen) - len(topN)
	if remaining > 0 {
		sb.WriteString(fmt.Sprintf("\n\n  ... 还有 %d 项，优先级较低，暂不列出", remaining))
	}
}

// writeFooter 写报告落款（生成时间）。
func writeFooter(sb *strings.Builder, t time.Time) {
	sb.WriteString(fmt.Sprintf(footerFmt, t.Format("2006-01-02 15:04:05")))
}