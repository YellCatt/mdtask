package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("任务不存在")

const (
	FID       = "ID"
	FTitle    = "标题"
	FStatus   = "状态"
	FPriority = "优先级"
	FDue      = "截止日期"
	FNote     = "备注"
	FAdded    = "添加日期"
	FDoneAt   = "完成时间"
)

var stdColumns = []string{FStatus, FID, FTitle, FPriority, FDue, FNote, FAdded}

var fieldAliases = map[string]string{
	"id": FID, "编号": FID, "序号": FID, "no": FID,
	"标题": FTitle, "title": FTitle, "任务": FTitle, "名称": FTitle, "name": FTitle, "内容": FTitle,
	"状态": FStatus, "status": FStatus, "state": FStatus, "进度": FStatus,
	"优先级": FPriority, "priority": FPriority, "pri": FPriority, "级别": FPriority, "重要度": FPriority,
	"截止日期": FDue, "截止": FDue, "due": FDue, "duedate": FDue, "deadline": FDue, "日期": FDue, "date": FDue,
	"备注": FNote, "note": FNote, "notes": FNote, "说明": FNote, "描述": FNote, "desc": FNote, "description": FNote,
	"添加日期": FAdded, "添加": FAdded, "added": FAdded, "created": FAdded, "created_at": FAdded, "创建日期": FAdded,
	"完成时间": FDoneAt, "完成": FDoneAt, "doneat": FDoneAt, "done_at": FDoneAt, "finished": FDoneAt, "完成日期": FDoneAt,
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "", "_", "", "-", "", "\t", "",
		"（", "", "）", "", "(", "", ")", "").Replace(s)
}

func canonField(col string) string {
	if f, ok := fieldAliases[norm(col)]; ok {
		return f
	}
	return ""
}

type fileState struct {
	Path  string
	Name  string
	crlf  bool
	dirty bool
	lines []string
	main  *tbl
	arch  *tbl
}

type Store struct {
	mu        sync.Mutex
	Dir       string
	Backup    bool
	ArchTitle string
	files     []*fileState
	primary   *fileState
}

func NewStore(dir string, backup bool) *Store {
	title := os.Getenv("MDTASK_ARCHIVE_HEADING")
	if title == "" {
		title = "归档"
	}
	return &Store{Dir: dir, Backup: backup, ArchTitle: title}
}

func (s *Store) Init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return err
	}
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Load() error {
	s.files = nil
	s.primary = nil

	ents, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(s.Dir, 0o755); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	var mdFiles []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			mdFiles = append(mdFiles, name)
		}
	}
	sort.Strings(mdFiles)

	if len(mdFiles) == 0 {
		primaryPath := filepath.Join(s.Dir, "tasks.md")
		f, err := s.loadFile(primaryPath)
		if err != nil {
			return err
		}
		s.files = append(s.files, f)
		s.primary = f
		return nil
	}

	for _, name := range mdFiles {
		p := filepath.Join(s.Dir, name)
		f, err := s.loadFile(p)
		if err != nil {
			return err
		}
		s.files = append(s.files, f)
	}
	s.primary = s.files[0]
	return nil
}

func (s *Store) loadFile(path string) (*fileState, error) {
	f := &fileState{Path: path, Name: filepath.Base(path)}
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		b = []byte(defaultDoc())
		f.dirty = true
	}
	raw := string(b)
	f.crlf = strings.Count(raw, "\r\n") > 0
	f.lines = strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	a, b2 := locateTableFrom(f.lines, 0)
	if a < 0 {
		s.createMainTableIn(f)
		a, b2 = locateTableFrom(f.lines, 0)
	}
	f.main = &tbl{start: a, end: b2}
	if f.main.parse(f.lines) {
		f.dirty = true
	}
	for i := range f.main.tasks {
		f.main.tasks[i].Source = path
	}

	if hi := findHeadingFrom(f.lines, s.ArchTitle, f.main.end); hi >= 0 {
		if x, y := locateTableFrom(f.lines, hi+1); x >= 0 {
			f.arch = &tbl{start: x, end: y}
			if f.arch.parse(f.lines) {
				f.dirty = true
			}
			for i := range f.arch.tasks {
				f.arch.tasks[i].Source = path
			}
		}
	}
	return f, nil
}

func (s *Store) createMainTableIn(f *fileState) {
	blank := true
	for _, l := range f.lines {
		if strings.TrimSpace(l) != "" {
			blank = false
			break
		}
	}
	if blank {
		f.lines = strings.Split(strings.TrimRight(defaultDoc(), "\n"), "\n")
		return
	}
	if strings.TrimSpace(f.lines[len(f.lines)-1]) != "" {
		f.lines = append(f.lines, "")
	}
	f.lines = append(f.lines, headerRows(stdColumns)...)
	f.dirty = true
}

func (s *Store) List() ([]Task, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, nil, err
	}
	var all []Task
	var cols []string
	for _, f := range s.files {
		if f.main == nil {
			continue
		}
		all = append(all, cloneTasks(f.main.tasks)...)
		if len(cols) == 0 {
			cols = append([]string(nil), f.main.Columns...)
		}
	}
	return all, cols, nil
}

func (s *Store) ListArchive() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, err
	}
	var all []Task
	for _, f := range s.files {
		if f.arch != nil {
			all = append(all, cloneTasks(f.arch.tasks)...)
		}
	}
	return all, nil
}

func (s *Store) Update(fn func(st *Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Archive(pred func(Task) bool) int {
	total := 0
	today := Today().Format("2006-01-02")
	for _, f := range s.files {
		if f.main == nil {
			continue
		}
		rest := make([]Task, 0, len(f.main.tasks))
		var hit []Task
		for _, t := range f.main.tasks {
			if pred(t) {
				if t.DoneAt == "" {
					t.DoneAt = today
				}
				hit = append(hit, t)
			} else {
				rest = append(rest, t)
			}
		}
		if len(hit) == 0 {
			continue
		}
		s.ensureArchiveIn(f)
		f.arch.tasks = append(f.arch.tasks, hit...)
		f.main.tasks = rest
		f.dirty = true
		total += len(hit)
	}
	return total
}

func (s *Store) AddTask(t Task) error {
	if s.primary == nil {
		return errors.New("没有可用的任务文件")
	}
	t.Source = s.primary.Path
	s.primary.main.tasks = append(s.primary.main.tasks, t)
	s.primary.dirty = true
	return nil
}

func (s *Store) UpdateTask(id string, fn func(*Task) error) error {
	for _, f := range s.files {
		for i := range f.main.tasks {
			if f.main.tasks[i].ID == id {
				if err := fn(&f.main.tasks[i]); err != nil {
					return err
				}
				f.main.tasks[i].Source = f.Path
				f.dirty = true
				return nil
			}
		}
	}
	return ErrNotFound
}

func (s *Store) SetTaskStatus(id, status string) error {
	for _, f := range s.files {
		for i := range f.main.tasks {
			if f.main.tasks[i].ID == id {
				f.main.tasks[i].Status = status
				f.dirty = true
				return nil
			}
		}
	}
	return ErrNotFound
}

func (s *Store) RemoveTask(id string) error {
	for _, f := range s.files {
		for i := range f.main.tasks {
			if f.main.tasks[i].ID == id {
				f.main.tasks = append(f.main.tasks[:i], f.main.tasks[i+1:]...)
				f.dirty = true
				return nil
			}
		}
	}
	return ErrNotFound
}

func (s *Store) ensureArchiveIn(f *fileState) {
	if f.arch != nil {
		hasDoneAt := false
		for _, c := range f.arch.Columns {
			if canonField(c) == FDoneAt {
				hasDoneAt = true
				break
			}
		}
		if !hasDoneAt {
			f.arch.Columns = append(f.arch.Columns, FDoneAt)
			f.arch.fields = append(f.arch.fields, FDoneAt)
			f.dirty = true
		}
		return
	}
	cols := append([]string(nil), f.main.Columns...)
	if len(cols) == 0 {
		cols = append([]string(nil), stdColumns...)
	}
	hasDoneAt := false
	for _, c := range cols {
		if canonField(c) == FDoneAt {
			hasDoneAt = true
			break
		}
	}
	if !hasDoneAt {
		cols = append(cols, FDoneAt)
	}
	if strings.TrimSpace(f.lines[len(f.lines)-1]) != "" {
		f.lines = append(f.lines, "")
	}
	f.lines = append(f.lines, "## "+s.ArchTitle, "")
	start := len(f.lines)
	f.lines = append(f.lines, headerRows(cols)...)

	f.arch = &tbl{start: start, end: len(f.lines), Columns: cols}
	for _, c := range cols {
		f.arch.fields = append(f.arch.fields, canonField(c))
	}
	f.dirty = true
}

func (s *Store) NextID() string {
	max := 0
	scan := func(ts []Task) {
		for _, t := range ts {
			if n, err := strconv.Atoi(strings.TrimSpace(t.ID)); err == nil && n > max {
				max = n
			}
		}
	}
	for _, f := range s.files {
		if f.main != nil {
			scan(f.main.tasks)
		}
		if f.arch != nil {
			scan(f.arch.tasks)
		}
	}
	return strconv.Itoa(max + 1)
}

func (s *Store) TouchAddedDates(known map[string]bool) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return known, err
	}
	today := Today().Format("2006-01-02")
	for _, f := range s.files {
		if f.main != nil {
			for i := range f.main.tasks {
				t := &f.main.tasks[i]
				if t.ID != "" {
					if !known[t.ID] && t.Added == "" {
						t.Added = today
						f.dirty = true
					}
					known[t.ID] = true
				}
			}
		}
		if f.arch != nil {
			for i := range f.arch.tasks {
				t := &f.arch.tasks[i]
				if t.ID != "" {
					if !known[t.ID] && t.Added == "" {
						t.Added = today
						f.dirty = true
					}
					known[t.ID] = true
				}
			}
		}
	}
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return known, err
			}
		}
	}
	return known, nil
}

func (s *Store) CollectKnownIDs() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, f := range s.files {
		if f.main != nil {
			for _, t := range f.main.tasks {
				if t.ID != "" {
					known[t.ID] = true
				}
			}
		}
		if f.arch != nil {
			for _, t := range f.arch.tasks {
				if t.ID != "" {
					known[t.ID] = true
				}
			}
		}
	}
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return nil, err
			}
		}
	}
	return known, nil
}

func (s *Store) flushFile(f *fileState) error {
	type repl struct {
		start, end int
		rows       []string
	}
	var rs []repl
	if f.main != nil {
		rs = append(rs, repl{f.main.start, f.main.end, f.main.render()})
	}
	if f.arch != nil {
		rs = append(rs, repl{f.arch.start, f.arch.end, f.arch.render()})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].start > rs[j].start })

	out := append([]string(nil), f.lines...)
	for _, r := range rs {
		nl := make([]string, 0, len(out)+len(r.rows))
		nl = append(nl, out[:r.start]...)
		nl = append(nl, r.rows...)
		nl = append(nl, out[r.end:]...)
		out = nl
	}

	nlSep := "\n"
	if f.crlf {
		nlSep = "\r\n"
	}
	if s.Backup {
		backupFile(f.Path)
	}
	f.dirty = false
	return atomicWrite(f.Path, []byte(strings.Join(out, nlSep)))
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mdtask-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	os.Chmod(name, 0o644)
	if err := os.Rename(name, path); err != nil {
		if rmErr := os.Remove(path); rmErr == nil {
			if err2 := os.Rename(name, path); err2 == nil {
				return nil
			}
		}
		os.Remove(name)
		return err
	}
	return nil
}

func backupFile(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	dir := filepath.Join(filepath.Dir(path), ".mdtask-backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := time.Now().Format("20060102-150405.000") + ".md"
	os.WriteFile(filepath.Join(dir, name), src, 0o644)

	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) <= 10 {
		return
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-10] {
		os.Remove(filepath.Join(dir, n))
	}
}