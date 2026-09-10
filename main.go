// MDTask — 用 Markdown 表格管理任务的命令行工具。
// 数据只存在 md 文件里，表格之外的内容（标题、说明、其它段落）原样保留。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const usage = `MDTask — 用 Markdown 表格管理任务

用法:
  mdtask [选项] [命令] [参数]

选项（要写在命令之前）:
  -file <路径>     任务 md 文件，默认取 config.yaml 里的 file
  -config <路径>   配置文件，默认依次找 ./config.yaml、./config.yml
  -no-backup       关闭写前自动备份

状态（md 的状态列里就写这些 emoji，写中文/英文也能识别）:
  ✅ 完成      done      结束态，可归档
  ⏸️ 进行中    doing
  ❌ 停滞      hold
  🔴 取消      cancel    结束态，可归档
  留空        待办       新建任务的默认状态

命令:
  ls                       列出任务（无参数时默认执行）
      -s <状态>            按状态筛选
      -p <优先级>          按优先级筛选
      -q <关键词>          搜索全部字段
      -a                   连同「归档」章节一起显示
  add <标题>               新增任务
      -s <状态> -p <优先级> -d <日期> -n <备注>
  show <id>                查看任务详情
  edit <id>                修改任务（只改显式给出的字段）
      -t <标题> -s <状态> -p <优先级> -d <日期> -n <备注>
  mark <id> <状态>         设置状态，状态可写 emoji / 英文 / 中文
  done <id...>             标记 ✅ 完成
  doing <id...>            标记 ⏸️ 进行中
  hold <id...>             标记 ❌ 停滞
  cancel <id...>           标记 🔴 取消
  todo <id...>             清空状态，回到待办
  archive                  把结束态任务搬进「## 归档」章节
      -before <日期>       只归档截止日期早于该日期的
      -days <N>            只归档截止日期在 N 天之前的
      -all                 连 ❌ 停滞 也一起归档
                           设 MDTASK_AUTO_ARCHIVE=0 后，每次改状态都会自动归档
                           结束态任务；=7 表示只归档截止日期在 7 天前的
  rm <id...>               删除任务
  daemon                   常驻后台，到点生成日报并弹通知
      -at 21:00,05:00      日报时间（默认 21:00 与 05:00）
      -interval 60         扫描 md 变化的间隔（秒）
      -open                生成后顺便打开日报文件
      -once                立刻出一份日报并退出（调试用）
  install                  把 daemon 装进开机启动项
  uninstall                移除开机启动项
  init                     在当前目录生成一份带注释的默认 config.yaml
      -force               已存在时覆盖
  path                     打印 md 文件的绝对路径
  open                     用系统默认程序打开 md 文件
  help                     显示本帮助

示例:
  mdtask add "写 Go 小项目" -p high -d 2026-09-15 -n "基于 md 文件驱动"
  mdtask mark 2 ⏸️
  mdtask done 2
  mdtask archive -days 7
  mdtask ls -a
`

// ---------- 颜色 ----------

const (
	cReset = "\033[0m"
	cDim   = "\033[2m"
	cGray  = "\033[90m"
	cRed   = "\033[31m"
	cGreen = "\033[32m"
	cYel   = "\033[33m"
	cBlue  = "\033[34m"
	cBold  = "\033[1m"
)

var useColor bool

func initColor() {
	switch cfg.Color {
	case "always":
		useColor = true
		return
	case "never":
		useColor = false
		return
	}
	if os.Getenv("NO_COLOR") != "" {
		return
	}
	if os.Getenv("MDTASK_COLOR") != "" {
		useColor = true
		return
	}
	// Windows 老控制台默认不支持 ANSI，其余终端跟随 TTY 判断
	if runtime.GOOS == "windows" {
		return
	}
	fi, err := os.Stdout.Stat()
	useColor = err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func paint(c, s string) string {
	if c == "" || !useColor || s == "" {
		return s
	}
	return c + s + cReset
}

// ---------- 终端宽度（emoji / 中文按 2 列） ----------

func runeWidth(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r >= 0xfe00 && r <= 0xfe0f: // 变体选择符，不占宽
		return 0
	case r >= 0x1f000 && r <= 0x1faff, // 🔴
		r >= 0x2600 && r <= 0x27bf,    // ✅ ❌
		r >= 0x2b00 && r <= 0x2bff,    // ⬜
		r >= 0x2300 && r <= 0x23ff:    // ⏸
		return 2
	}
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x20000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}

func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func pad(s string, w int) string {
	n := w - dispWidth(s)
	if n < 0 {
		n = 0
	}
	return s + strings.Repeat(" ", n)
}

func truncate(s string, max int) string {
	if dispWidth(s) <= max {
		return s
	}
	w := 0
	out := make([]rune, 0, max)
	for _, r := range s {
		d := runeWidth(r)
		if w+d > max-1 {
			break
		}
		w += d
		out = append(out, r)
	}
	return string(out) + "…"
}

// ---------- 入口 ----------

var st *Store

func main() {
	file, config, noBackup, rest := splitArgs(os.Args[1:])
	loadConfig(config)

	if file == "" {
		file = os.Getenv("MDTASK_FILE")
	}
	if file == "" {
		file = cfg.File
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		fatal(err)
	}

	initColor()
	st = NewStore(abs, cfg.Backup && !noBackup)
	if h := os.Getenv("MDTASK_ARCHIVE_HEADING"); h != "" {
		st.archTitle = h
	} else {
		st.archTitle = cfg.Archive.Heading
	}
	initAutoArchive()

	if err := st.Init(); err != nil {
		fatal(fmt.Errorf("打开 %s 失败: %w", abs, err))
	}

	cmd := "ls"
	args := rest
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		cmd, args = rest[0], rest[1:]
	}

	switch cmd {
	case "ls", "list", "l":
		cmdList(args)
	case "add", "new", "a":
		cmdAdd(args)
	case "show", "s":
		cmdShow(args)
	case "edit", "e":
		cmdEdit(args)
	case "mark", "m", "st":
		cmdMark(args)
	case "done":
		cmdSetStatus(stDone, args)
	case "doing":
		cmdSetStatus(stDoing, args)
	case "hold":
		cmdSetStatus(stHold, args)
	case "cancel", "drop":
		cmdSetStatus(stCancel, args)
	case "todo":
		cmdSetStatus("", args)
	case "archive", "arch":
		cmdArchive(args)
	case "rm", "del", "remove":
		cmdRemove(args)
	case "daemon", "d":
		cmdDaemon(args)
	case "install":
		cmdInstall(args)
	case "uninstall":
		cmdUninstall(args)
	case "init":
		cmdInit(args)
	case "path":
		fmt.Println(st.path)
	case "open":
		openFile(st.path)
	case "help", "-h", "--help", "-help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func fatal(v any) {
	fmt.Fprintln(os.Stderr, "错误:", v)
	os.Exit(1)
}

func splitArgs(args []string) (file, config string, noBackup bool, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-file" || a == "--file" || a == "-f":
			if i+1 < len(args) {
				file = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-file="):
			file = strings.TrimPrefix(a, "-file=")
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case strings.HasPrefix(a, "-f="):
			file = strings.TrimPrefix(a, "-f=")
		case a == "-config" || a == "--config" || a == "-c":
			if i+1 < len(args) {
				config = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-config="):
			config = strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--config="):
			config = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			config = strings.TrimPrefix(a, "-c=")
		case a == "-no-backup" || a == "--no-backup":
			noBackup = true
		default:
			rest = append(rest, a)
		}
	}
	return
}

// ---------- 命令 ----------

func cmdList(args []string) {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	status := fs.String("s", "", "按状态筛选")
	prio := fs.String("p", "", "按优先级筛选")
	query := fs.String("q", "", "关键词搜索")
	withArch := fs.Bool("a", false, "连同归档一起显示")
	fs.Parse(args)

	tasks, _, err := st.List()
	if err != nil {
		fatal(err)
	}
	if *status != "" {
		key := canonicalStatus(*status)
		tasks = filterTasks(tasks, func(t Task) bool { return canonicalStatus(t.Status) == key })
	}
	if *prio != "" {
		key := strings.ToLower(strings.TrimSpace(*prio))
		tasks = filterTasks(tasks, func(t Task) bool { return strings.ToLower(t.Priority) == key })
	}
	if *query != "" {
		key := strings.ToLower(strings.TrimSpace(*query))
		tasks = filterTasks(tasks, func(t Task) bool {
			return strings.Contains(strings.ToLower(allText(t)), key)
		})
	}
	sortTasks(tasks)
	printTable(tasks, true)

	if *withArch {
		arch, err := st.ListArchive()
		if err != nil {
			fatal(err)
		}
		if len(arch) == 0 {
			fmt.Println(paint(cGray, "（归档为空）"))
			return
		}
		fmt.Println()
		fmt.Println(paint(cBold, "── 归档 ──"))
		sortTasks(arch)
		printTable(arch, false)
	}
}

func cmdAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	status := fs.String("s", "", "状态")
	prio := fs.String("p", "mid", "优先级")
	due := fs.String("d", "", "截止日期")
	note := fs.String("n", "", "备注")
	fs.Parse(args)

	title := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(title) == "" {
		fmt.Fprintln(os.Stderr, "用法: mdtask add <标题> [-s 状态] [-p 优先级] [-d 日期] [-n 备注]")
		os.Exit(2)
	}
	key := canonicalStatus(*status)

	var id string
	err := st.Update(func(s *Store) error {
		id = s.nextID()
		s.main.tasks = append(s.main.tasks, Task{
			ID: id, Title: title, Status: key,
			Priority: *prio, Due: *due, Note: *note,
		})
		return nil
	})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("已添加 #%s %s %s\n", paint(cBold, id), title, paint(statusColor(key), statusLabel(key)))
}

func cmdShow(args []string) {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.Parse(args)
	if len(fs.Args()) == 0 {
		fmt.Fprintln(os.Stderr, "用法: mdtask show <id>")
		os.Exit(2)
	}
	tasks, _, err := st.List()
	if err != nil {
		fatal(err)
	}
	for _, raw := range fs.Args() {
		t, ok := findTask(tasks, raw)
		if !ok {
			fmt.Fprintf(os.Stderr, "未找到任务: %s\n", raw)
			continue
		}
		fmt.Printf("ID        %s\n", t.ID)
		fmt.Printf("标题      %s\n", t.Title)
		fmt.Printf("状态      %s\n", paint(statusColor(t.Status), statusLabel(t.Status)))
		fmt.Printf("优先级    %s\n", paint(prioColor(t.Priority), prioLabel(t.Priority)))
		fmt.Printf("截止日期  %s\n", dueText(t, t.Due))
		fmt.Printf("备注      %s\n", t.Note)
		for _, kv := range sortedExtra(t.Extra) {
			fmt.Printf("%s  %s\n", pad(kv[0], 9), kv[1])
		}
		fmt.Println()
	}
}

func cmdEdit(args []string) {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	title := fs.String("t", "", "标题")
	status := fs.String("s", "", "状态")
	prio := fs.String("p", "", "优先级")
	due := fs.String("d", "", "截止日期")
	note := fs.String("n", "", "备注")
	fs.Parse(args)

	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "用法: mdtask edit <id> [-t 标题] [-s 状态] [-p 优先级] [-d 日期] [-n 备注]")
		os.Exit(2)
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if len(set) == 0 {
		fmt.Fprintln(os.Stderr, "没有给出要修改的字段")
		os.Exit(2)
	}

	if err := st.Update(func(s *Store) error {
		for i := range s.main.tasks {
			if s.main.tasks[i].ID != id {
				continue
			}
			if set["t"] {
				s.main.tasks[i].Title = *title
			}
			if set["s"] {
				s.main.tasks[i].Status = canonicalStatus(*status)
			}
			if set["p"] {
				s.main.tasks[i].Priority = *prio
			}
			if set["d"] {
				s.main.tasks[i].Due = *due
			}
			if set["n"] {
				s.main.tasks[i].Note = *note
			}
			return nil
		}
		return errNotFound{id}
	}); err != nil {
		fatal(err)
	}
	fmt.Printf("已更新 #%s\n", id)
	tryAutoArchive()
}

// cmdMark 用任意写法设置状态：mdtask mark 2 ⏸️ / doing / 进行中
func cmdMark(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: mdtask mark <id> <状态>\n"+
			"状态: ✅完成 / ⏸️进行中 / ❌停滞 / 🔴取消，也可写 done / doing / hold / cancel")
		os.Exit(2)
	}
	id, raw := args[0], strings.Join(args[1:], " ")
	if statusDefOf(raw) == nil {
		fmt.Fprintf(os.Stderr, "未知状态: %s\n", raw)
		os.Exit(2)
	}
	cmdSetStatus(canonicalStatus(raw), []string{id})
}

func cmdSetStatus(key string, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "用法: mdtask %s <id...>\n", cmdNameOfStatus(key))
		os.Exit(2)
	}
	if err := st.Update(func(s *Store) error {
		for _, id := range args {
			found := false
			for i := range s.main.tasks {
				if s.main.tasks[i].ID == id {
					s.main.tasks[i].Status = key
					found = true
					break
				}
			}
			if !found {
				return errNotFound{id}
			}
		}
		return nil
	}); err != nil {
		fatal(err)
	}
	fmt.Printf("已标记 #%s 为 %s\n", strings.Join(args, ", #"),
		paint(statusColor(key), statusLabel(key)))
	tryAutoArchive()
}

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

func cmdNameOfStatus(key string) string {
	for i := range statusDefs {
		if statusDefs[i].Key == key {
			return strings.ToLower(statusDefs[i].Alias[0])
		}
	}
	return "todo"
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

func cmdRemove(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: mdtask rm <id...>")
		os.Exit(2)
	}
	if err := st.Update(func(s *Store) error {
		for _, id := range args {
			found := -1
			for i := range s.main.tasks {
				if s.main.tasks[i].ID == id {
					found = i
					break
				}
			}
			if found < 0 {
				return errNotFound{id}
			}
			s.main.tasks = append(s.main.tasks[:found], s.main.tasks[found+1:]...)
		}
		return nil
	}); err != nil {
		fatal(err)
	}
	fmt.Printf("已删除 #%s\n", strings.Join(args, ", #"))
}

// ---------- 输出 ----------

func printTable(tasks []Task, summary bool) {
	if len(tasks) == 0 {
		fmt.Println(paint(cDim, "（没有任务）"))
		return
	}

	headers := []string{"状态", "ID", "优先级", "标题", "截止日期", "备注"}
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{
			statusLabel(t.Status),
			t.ID,
			prioLabel(t.Priority),
			truncate(oneLine(t.Title), 44),
			t.Due,
			truncate(oneLine(t.Note), 30),
		})
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = dispWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if w := dispWidth(c); w > widths[i] {
				widths[i] = w
			}
		}
	}

	head := make([]string, len(headers))
	for i, h := range headers {
		head[i] = paint(cBold, pad(h, widths[i]))
	}
	fmt.Println(strings.Join(head, "  "))

	for i, r := range rows {
		t := tasks[i]
		cells := make([]string, len(r))
		for j, c := range r {
			cells[j] = pad(c, widths[j])
		}
		closed := isClosedStatus(t.Status)
		cells[0] = pad(paint(statusColor(t.Status), statusLabel(t.Status)), widths[0])
		cells[1] = paint(cDim, cells[1])
		cells[2] = pad(paint(prioColor(t.Priority), prioLabel(t.Priority)), widths[2])
		cells[4] = pad(paint(dueColor(t, t.Due), t.Due), widths[4])
		cells[5] = paint(cGray, cells[5])
		if closed {
			cells[3] = paint(cDim, cells[3])
		}
		fmt.Println(strings.Join(cells, "  "))
	}

	if summary {
		n := map[string]int{}
		for _, t := range tasks {
			if d := statusDefOf(t.Status); d != nil {
				n[d.Key]++
			} else {
				n["待办"]++
			}
		}
		parts := make([]string, 0, len(statusDefs)+1)
		if n["待办"] > 0 {
			parts = append(parts, fmt.Sprintf("待办 %d", n["待办"]))
		}
		for i := range statusDefs {
			if n[statusDefs[i].Key] > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", statusDefs[i].Label, n[statusDefs[i].Key]))
			}
		}
		fmt.Println()
		fmt.Println(paint(cGray, fmt.Sprintf("共 %d 项 · %s", len(tasks), strings.Join(parts, " · "))))
	}
}

func dueText(t Task, v string) string {
	if v == "" {
		return paint(cGray, "（空）")
	}
	return paint(dueColor(t, v), v)
}

// ---------- 状态 / 优先级显示 ----------

func statusLabel(v string) string {
	if d := statusDefOf(v); d != nil {
		return d.Key + " " + d.Label // md 里存 emoji，终端补上中文名
	}
	s := strings.TrimSpace(v)
	if s == "" || strings.ToLower(s) == "todo" { // 空状态（含旧写法 todo）= 待办
		return "⬜ 待办"
	}
	return "• " + s
}

func statusColor(v string) string {
	if d := statusDefOf(v); d != nil {
		switch d.Label {
		case "完成":
			return cGreen
		case "进行中":
			return cBlue
		case "停滞":
			return cYel
		case "取消":
			return cDim
		}
	}
	return "" // 待办 / 未知状态不染色
}

func prioLabel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high":
		return "高"
	case "mid":
		return "中"
	case "low":
		return "低"
	}
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func prioColor(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high":
		return cRed
	case "mid":
		return cYel
	case "low":
		return cGray
	}
	return cGray
}

func dueColor(t Task, v string) string {
	if v == "" || isClosedStatus(t.Status) {
		return cGray
	}
	if d, err := time.Parse("2006-01-02", strings.TrimSpace(v)); err == nil {
		today := time.Now().Truncate(24 * time.Hour)
		if d.Before(today) {
			return cRed
		}
	}
	return cGray
}

// ---------- 辅助 ----------

func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func allText(t Task) string {
	var b strings.Builder
	b.WriteString(t.ID + " " + t.Title + " " + t.Status + " " + t.Priority + " " + t.Due + " " + t.Note)
	for _, kv := range sortedExtra(t.Extra) {
		b.WriteString(" " + kv[1])
	}
	return b.String()
}

func sortedExtra(m map[string]string) [][2]string {
	if len(m) == 0 {
		return nil
	}
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func filterTasks(tasks []Task, f func(Task) bool) []Task {
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if f(t) {
			out = append(out, t)
		}
	}
	return out
}

func sortTasks(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := statusRankOf(tasks[i].Status), statusRankOf(tasks[j].Status)
		if a != b {
			return a < b
		}
		a, b = prioRank(tasks[i]), prioRank(tasks[j])
		if a != b {
			return a > b
		}
		return dueKey(tasks[i].Due) < dueKey(tasks[j].Due)
	})
}

func prioRank(t Task) int {
	switch strings.ToLower(strings.TrimSpace(t.Priority)) {
	case "high":
		return 3
	case "mid":
		return 2
	case "low":
		return 1
	}
	return 0
}

func dueKey(v string) string {
	if strings.TrimSpace(v) == "" {
		return "9999-99-99"
	}
	return v
}

func findTask(tasks []Task, id string) (Task, bool) {
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

func openFile(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}
