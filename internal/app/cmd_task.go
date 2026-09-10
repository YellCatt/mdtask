package app

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"mdtask/internal/config"
	"mdtask/internal/status"
	"mdtask/internal/store"
	"mdtask/internal/ui"
)

func CmdList(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	sf := fs.String("s", "", "按状态筛选")
	pf := fs.String("p", "", "按优先级筛选")
	qf := fs.String("q", "", "关键词搜索")
	af := fs.Bool("a", false, "连同归档一起显示")
	if err := fs.Parse(args); err != nil {
		return err
	}

	tasks, _, err := st.List()
	if err != nil {
		return err
	}
	if *sf != "" {
		key := status.Canonical(*sf)
		tasks = ui.FilterTasks(tasks, func(t store.Task) bool { return status.Canonical(t.Status) == key })
	}
	if *pf != "" {
		key := strings.ToLower(strings.TrimSpace(*pf))
		tasks = ui.FilterTasks(tasks, func(t store.Task) bool { return strings.ToLower(t.Priority) == key })
	}
	if *qf != "" {
		key := strings.ToLower(strings.TrimSpace(*qf))
		tasks = ui.FilterTasks(tasks, func(t store.Task) bool {
			return strings.Contains(strings.ToLower(ui.AllText(t)), key)
		})
	}
	ui.SortTasks(tasks)
	ui.PrintTable(tasks, true)

	if *af {
		arch, err := st.ListArchive()
		if err != nil {
			return err
		}
		if len(arch) == 0 {
			fmt.Println("（归档为空）")
			return nil
		}
		fmt.Println()
		fmt.Println("── 归档 ──")
		ui.SortTasks(arch)
		ui.PrintTable(arch, false)
	}
	return nil
}

func CmdAdd(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	sf := fs.String("s", "", "状态")
	pf := fs.String("p", "mid", "优先级")
	df := fs.String("d", "", "截止日期")
	nf := fs.String("n", "", "备注")
	if err := fs.Parse(args); err != nil {
		return err
	}

	title := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("用法: mdtask add <标题> [-s 状态] [-p 优先级] [-d 日期] [-n 备注]")
	}
	key := status.Canonical(*sf)

	id := st.NextID()
	err := st.AddTask(store.Task{
		ID: id, Title: title, Status: key,
		Priority: *pf, Due: *df, Note: *nf,
	})
	if err != nil {
		return err
	}
	fmt.Printf("已添加 #%s %s\n", id, title)
	return nil
}

func CmdShow(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) == 0 {
		return fmt.Errorf("用法: mdtask show <id>")
	}
	tasks, _, err := st.List()
	if err != nil {
		return err
	}
	for _, raw := range fs.Args() {
		t, ok := ui.FindTask(tasks, raw)
		if !ok {
			fmt.Fprintf(os.Stderr, "未找到任务 %s\n", raw)
			continue
		}
		fmt.Printf("ID        %s\n", t.ID)
		fmt.Printf("标题      %s\n", t.Title)
		fmt.Printf("状态      %s\n", status.Label(t.Status))
		fmt.Printf("优先级    %s\n", t.Priority)
		fmt.Printf("截止日期  %s\n", ui.DueText(t))
		fmt.Printf("备注      %s\n", t.Note)
		for _, kv := range ui.SortedExtra(t.Extra) {
			fmt.Printf("%-9s %s\n", kv[0], kv[1])
		}
		fmt.Println()
	}
	return nil
}

func CmdEdit(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	tf := fs.String("t", "", "标题")
	sf := fs.String("s", "", "状态")
	pf := fs.String("p", "", "优先级")
	df := fs.String("d", "", "截止日期")
	nf := fs.String("n", "", "备注")
	if err := fs.Parse(args); err != nil {
		return err
	}

	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("用法: mdtask edit <id> [-t 标题] [-s 状态] [-p 优先级] [-d 日期] [-n 备注]")
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if len(set) == 0 {
		return fmt.Errorf("没有给出要修改的字段")
	}

	err := st.UpdateTask(id, func(t *store.Task) error {
		if set["t"] {
			t.Title = *tf
		}
		if set["s"] {
			t.Status = status.Canonical(*sf)
		}
		if set["p"] {
			t.Priority = *pf
		}
		if set["d"] {
			t.Due = *df
		}
		if set["n"] {
			t.Note = *nf
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("已更新 #%s\n", id)
	tryAutoArchive(st, cfg)
	return nil
}

func CmdMark(st *store.Store, cfg *config.Config, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("用法: mdtask mark <id> <状态>\n状态: done/doing/hold/cancel 或对应 emoji")
	}
	id, raw := args[0], strings.Join(args[1:], " ")
	if status.DefOf(raw) == nil {
		return fmt.Errorf("未知状态 %s", raw)
	}
	return CmdSetStatus(st, cfg, status.Canonical(raw), []string{id})
}

func CmdSetStatus(st *store.Store, cfg *config.Config, key string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: mdtask %s <id...>", status.CmdNameOf(key))
	}
	for _, id := range args {
		if err := st.SetTaskStatus(id, key); err != nil {
			return err
		}
	}
	fmt.Printf("已标记 #%s 为 %s\n", strings.Join(args, ", #"), status.Label(key))
	tryAutoArchive(st, cfg)
	return nil
}

func CmdRemove(st *store.Store, cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: mdtask rm <id...>")
	}
	for _, id := range args {
		if err := st.RemoveTask(id); err != nil {
			return err
		}
	}
	fmt.Printf("已删除 #%s\n", strings.Join(args, ", #"))
	return nil
}

func CmdPath(st *store.Store, cfg *config.Config, args []string) error {
	fmt.Println(st.Dir)
	return nil
}