package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// ---------- 任务的增删改查 ----------

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
