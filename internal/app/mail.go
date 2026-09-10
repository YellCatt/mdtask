package app

import (
	"flag"
	"fmt"

	"mdtask/internal/config"
	"mdtask/internal/store"
)

func CmdMail(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("mail", flag.ExitOnError)
	subject := fs.String("subject", "MDTask 日报", "邮件主题")
	body := fs.String("body", "", "邮件正文")
	to := fs.String("to", "", "收件人（逗号分隔）")
	attach := fs.String("attach", "", "附件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}

	_ = subject
	_ = body
	_ = to
	_ = attach

	tasks, _, err := st.List()
	if err != nil {
		return err
	}
	fmt.Printf("当前任务数: %d (mail 功能待实现)\n", len(tasks))
	return nil
}