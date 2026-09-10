// MDTask — 用 Markdown 表格管理任务的命令行工具。
// 数据只存在 md 文件里，表格之外的内容（标题、说明、其它段落）原样保留。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
	case "report", "r", "rp":
		cmdReport(args)
	case "mail", "ipmail":
		cmdMail(args)
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

// splitArgs 挑出写在命令之前的 -file / -config / -no-backup，其余原样交给子命令
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
