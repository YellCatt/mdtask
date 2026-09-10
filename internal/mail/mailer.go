package mail

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"mdtask/internal/config"
)

func Send(cfg *config.Config, subject, body string) error {
	if cfg.Mail.SMTPHost == "" || cfg.Mail.FromEmail == "" || cfg.Mail.AuthCode == "" || cfg.Mail.ToEmail == "" {
		return errors.New("邮件配置不完整：需要 smtp_host、from_email、auth_code、to_email")
	}
	port := cfg.Mail.SMTPPort
	if port == 0 {
		port = 465
	}
	timeout := cfg.Mail.Timeout
	if timeout == 0 {
		timeout = 15
	}

	addr := net.JoinHostPort(cfg.Mail.SMTPHost, fmt.Sprintf("%d", port))
	dialer := &net.Dialer{Timeout: time.Duration(timeout) * time.Second}

	var conn net.Conn
	var err error
	if port == 465 {
		tlsCfg := &tls.Config{
			ServerName:         cfg.Mail.SMTPHost,
			InsecureSkipVerify: cfg.Mail.TLSSkipVerify,
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}

	client, err := smtp.NewClient(conn, cfg.Mail.SMTPHost)
	if err != nil {
		return fmt.Errorf("SMTP 握手失败: %w", err)
	}
	defer client.Close()

	if port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{
				ServerName:         cfg.Mail.SMTPHost,
				InsecureSkipVerify: cfg.Mail.TLSSkipVerify,
			}
			if err := client.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}

	auth := smtp.PlainAuth("", cfg.Mail.FromEmail, cfg.Mail.AuthCode, cfg.Mail.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}

	if err := client.Mail(cfg.Mail.FromEmail); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}

	recipients := strings.Split(cfg.Mail.ToEmail, ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}
	for _, r := range recipients {
		if err := client.Rcpt(r); err != nil {
			return fmt.Errorf("设置收件人 %s 失败: %w", r, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("打开邮件数据流失败: %w", err)
	}

	msg := buildMessage(cfg.Mail.FromEmail, recipients, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("关闭邮件数据流失败: %w", err)
	}

	return client.Quit()
}

func buildMessage(from string, to []string, subject, body string) string {
	now := time.Now().Format(time.RFC1123Z)
	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", strings.Join(to, ", ")),
		fmt.Sprintf("Subject: =?UTF-8?B?%s?=", b64(subject)),
		fmt.Sprintf("Date: %s", now),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n"
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}