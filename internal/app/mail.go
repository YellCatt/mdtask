package app

import (
	"flag"
	"fmt"

	"mdtask/internal/config"
	"mdtask/internal/mail"
	"mdtask/internal/report"
	"mdtask/internal/store"
)

func CmdMail(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("mail", flag.ExitOnError)
	typeStr := fs.String("type", "daily", "报告类型: daily/weekly/monthly/yearly")
	customSubject := fs.String("subject", "", "自定义邮件主题（覆盖自动生成）")
	customBody := fs.String("body", "", "自定义邮件正文（覆盖报告内容）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var subject, body string
	var err error

	if *customBody != "" {
		body = *customBody
		if *customSubject != "" {
			subject = *customSubject
		} else {
			subject = "MDTask 自定义邮件"
		}
	} else {
		tasks, _, err2 := st.List()
		if err2 != nil {
			return err2
		}
		arch, _ := st.ListArchive()

		switch *typeStr {
		case "daily", "d":
			subject, body, err = report.BuildDaily(arch, tasks)
		case "weekly", "w":
			subject, body, err = report.BuildWeekly(arch, tasks)
		case "monthly", "m":
			subject, body, err = report.BuildMonthly(arch, tasks)
		case "yearly", "y":
			subject, body, err = report.BuildYearly(arch, tasks)
		default:
			subject, body, err = report.BuildDaily(arch, tasks)
		}
		if err != nil {
			return fmt.Errorf("生成报告失败: %w", err)
		}
		if *customSubject != "" {
			subject = *customSubject
		}
	}

	fmt.Printf("发送邮件到 %s ...\n", cfg.Mail.ToEmail)
	if err := mail.Send(cfg, subject, body); err != nil {
		return fmt.Errorf("发送邮件失败: %w", err)
	}
	fmt.Println("邮件发送成功")
	return nil
}