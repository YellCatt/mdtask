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
	pf := fs.String("p", "P2", "优先级")
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

const Usage = `MDTask — 基于 Markdown 的任务清单

用法:
  mdtask [-file <dir>] [-config <path>] [-no-backup] <命令> [参数]

命令:
  ls, list, l              列出任务  [-s 状态] [-p 优先级] [-q 关键词] [-a]
  add, new, a <标题>       添加任务  [-s 状态] [-p 优先级] [-d 日期] [-n 备注]
  show, s <id...>          查看详情
  edit, e <id>             修改任务  [-t 标题] [-s 状态] [-p 优先级] [-d 日期] [-n 备注]
  mark, m, st <id> <状态>  标记状态
  done|doing|hold|cancel|todo <id...>
                           快捷状态切换
  archive, arch            归档已关闭任务  [-before YYYY-MM-DD] [-days N] [-all]
  rm, del, remove <id...>  删除任务
  report, r, rp            生成报告  [-type daily|weekly|monthly|yearly] [-date YYYY-MM-DD] [-last] [-open]
  daemon, d                常驻后台  [-at HH:MM,HH:MM] [-interval 秒] [-open] [-once]
  mail, ipmail             发送邮件（待实现）
  install                  安装开机启动
  uninstall                卸载开机启动
  path                     显示数据目录
  open                     用默认程序打开数据目录
  help, -h, --help         显示本帮助

全局参数:
  -file, -f      指定数据目录
  -config, -c    指定配置文件
  -no-backup     本次运行不做备份

环境变量:
  MDTASK_FILE           默认数据目录
  MDTASK_CONFIG_PATH    默认配置文件路径
  MDTASK_AUTO_ARCHIVE   自动归档天数（0 表示关闭）
`