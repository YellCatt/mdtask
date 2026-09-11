package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"mdtask/internal/store"
)

const (
	sepHead   = "========================================"
	sepSub    = "------------------------------"
	footerFmt = "\n\n—— MDTask %s\n"
)

var prioOrder = []string{"P0", "P1", "P2", "P3", "P4"}

func sortByPrio(ts []store.Task) []store.Task {
	out := append([]store.Task(nil), ts...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := store.PriorityRank(out[i].Priority), store.PriorityRank(out[j].Priority)
		if ri != rj {
			return ri > rj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortDoneByDoneAt(ts []store.Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		return strings.TrimSpace(ts[i].DoneAt) < strings.TrimSpace(ts[j].DoneAt)
	})
}

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

func prioCountMap(tasks []store.Task) map[string]int {
	m := map[string]int{}
	for _, t := range tasks {
		p := strings.ToUpper(strings.TrimSpace(t.Priority))
		m[p]++
	}
	return m
}

func topByPrio(open []store.Task, n int) []store.Task {
	sorted := sortByPrio(open)
	if len(sorted) < n {
		n = len(sorted)
	}
	return sorted[:n]
}

func writeHeader(sb *strings.Builder, emoji, title string) {
	sb.WriteString(fmt.Sprintf("%s %s\n", emoji, title))
	sb.WriteString(sepHead)
	sb.WriteString("\n\n")
}

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
		}
		sb.WriteString(fmt.Sprintf(" #%s [%s] %s", t.ID, strings.TrimSpace(t.Priority), strings.TrimSpace(t.Title)))
		if strings.TrimSpace(t.Due) != "" {
			sb.WriteString(fmt.Sprintf("  (截止: %s)", strings.TrimSpace(t.Due)))
		}
		if strings.TrimSpace(t.Note) != "" {
			sb.WriteString(fmt.Sprintf("  — %s", strings.TrimSpace(t.Note)))
		}
	}
}

func writeOpenSection(sb *strings.Builder, label string, allOpen []store.Task, topN []store.Task) {
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("📋 %s: %d  —  最高优先的 %d 项\n", label, len(allOpen), len(topN)))
	sb.WriteString(sepSub)
	if len(allOpen) == 0 {
		sb.WriteString("\n（全部完成，没有待办！🎉）\n")
		return
	}
	for _, t := range topN {
		sb.WriteString(fmt.Sprintf("\n  #%s [%s] %s", t.ID, strings.TrimSpace(t.Priority), strings.TrimSpace(t.Title)))
		if strings.TrimSpace(t.Due) != "" {
			sb.WriteString(fmt.Sprintf("  (截止: %s)", strings.TrimSpace(t.Due)))
		}
		if strings.TrimSpace(t.Note) != "" {
			sb.WriteString(fmt.Sprintf("  — %s", strings.TrimSpace(t.Note)))
		}
	}
	remaining := len(allOpen) - len(topN)
	if remaining > 0 {
		sb.WriteString(fmt.Sprintf("\n\n  ... 还有 %d 项，优先级较低，暂不列出", remaining))
	}
}

func writeFooter(sb *strings.Builder, t time.Time) {
	sb.WriteString(fmt.Sprintf(footerFmt, t.Format("2006-01-02 15:04:05")))
}