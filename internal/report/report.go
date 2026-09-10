package report

import (
	"strings"
	"time"

	"mdtask/internal/store"
)

func prioRank(p string) int {
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

func sortByPrio(ts []store.Task) []store.Task {
	out := append([]store.Task(nil), ts...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := prioRank(out[i].Priority), prioRank(out[j].Priority)
		if ri != rj {
			return ri > rj
		}
		return out[i].ID < out[j].ID
	})
	return out
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

func openTopN(st *store.Store, n int) ([]store.Task, error) {
	open, _, err := st.List()
	if err != nil {
		return nil, err
	}
	sorted := sortByPrio(open)
	if len(sorted) < n {
		n = len(sorted)
	}
	return sorted[:n], nil
}

func archivedAll(st *store.Store) ([]store.Task, error) {
	return st.ListArchive()
}