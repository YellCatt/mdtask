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
	"mdtask/internal/logger"
)

func Send(cfg *config.Config, subject, body string) error {
	start := time.Now()

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

	recipients := strings.Split(cfg.Mail.ToEmail, ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}

	addr := net.JoinHostPort(cfg.Mail.SMTPHost, fmt.Sprintf("%d", port))
	dialer := &net.Dialer{Timeout: time.Duration(timeout) * time.Second}

	logger.Info("邮件发送开始",
		"addr", addr,
		"from", cfg.Mail.FromEmail,
		"to_count", len(recipients),
		"subject", subject,
		"port_mode", map[int]string{465: "TLS 直连"}[port],
	)

	var conn net.Conn
	var err error
	if port == 465 {
		tlsCfg := &tls.Config{
			ServerName:         cfg.Mail.SMTPHost,
			InsecureSkipVerify: cfg.Mail.TLSSkipVerify,
		}
		t0 := time.Now()
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if err != nil {
			logger.Error("TLS 连接 SMTP 服务器失败", "addr", addr, "cost_ms", time.Since(t0).Milliseconds(), "err", err)
			return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
		}
		logger.Debug("TLS 连接成功", "addr", addr, "cost_ms", time.Since(t0).Milliseconds())
	} else {
		t0 := time.Now()
		conn, err = dialer.Dial("tcp", addr)
		if err != nil {
			logger.Error("连接 SMTP 服务器失败", "addr", addr, "cost_ms", time.Since(t0).Milliseconds(), "err", err)
			return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
		}
		logger.Debug("TCP 连接成功", "addr", addr, "cost_ms", time.Since(t0).Milliseconds())
	}

	client, err := smtp.NewClient(conn, cfg.Mail.SMTPHost)
	if err != nil {
		logger.Error("SMTP 握手失败", "err", err)
		return fmt.Errorf("SMTP 握手失败: %w", err)
	}
	defer client.Close()
	logger.Debug("SMTP 客户端就绪")

	if port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{
				ServerName:         cfg.Mail.SMTPHost,
				InsecureSkipVerify: cfg.Mail.TLSSkipVerify,
			}
			t0 := time.Now()
			if err := client.StartTLS(tlsCfg); err != nil {
				logger.Error("STARTTLS 失败", "cost_ms", time.Since(t0).Milliseconds(), "err", err)
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
			logger.Debug("STARTTLS 完成", "cost_ms", time.Since(t0).Milliseconds())
		}
	}

	auth := smtp.PlainAuth("", cfg.Mail.FromEmail, cfg.Mail.AuthCode, cfg.Mail.SMTPHost)
	t0 := time.Now()
	if err := client.Auth(auth); err != nil {
		logger.Error("SMTP 认证失败", "cost_ms", time.Since(t0).Milliseconds(), "err", err)
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}
	logger.Debug("SMTP 认证成功", "cost_ms", time.Since(t0).Milliseconds())

	t0 = time.Now()
	if err := client.Mail(cfg.Mail.FromEmail); err != nil {
		logger.Error("设置发件人失败", "cost_ms", time.Since(t0).Milliseconds(), "err", err)
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	for _, r := range recipients {
		if err := client.Rcpt(r); err != nil {
			logger.Error("设置收件人失败", "rcpt", r, "err", err)
			return fmt.Errorf("设置收件人 %s 失败: %w", r, err)
		}
	}
	logger.Debug("收件人设置完成", "count", len(recipients), "cost_ms", time.Since(t0).Milliseconds())

	w, err := client.Data()
	if err != nil {
		logger.Error("打开邮件数据流失败", "err", err)
		return fmt.Errorf("打开邮件数据流失败: %w", err)
	}

	msg := buildMessage(cfg.Mail.FromEmail, recipients, subject, body)
	t0 = time.Now()
	if _, err := w.Write([]byte(msg)); err != nil {
		logger.Error("写入邮件内容失败", "cost_ms", time.Since(t0).Milliseconds(), "err", err)
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		logger.Error("关闭邮件数据流失败", "cost_ms", time.Since(t0).Milliseconds(), "err", err)
		return fmt.Errorf("关闭邮件数据流失败: %w", err)
	}
	logger.Debug("邮件内容发送完成", "cost_ms", time.Since(t0).Milliseconds())

	t0 = time.Now()
	if err := client.Quit(); err != nil {
		logger.Warn("SMTP Quit 失败", "cost_ms", time.Since(t0).Milliseconds(), "err", err)
	} else {
		logger.Debug("SMTP Quit 成功", "cost_ms", time.Since(t0).Milliseconds())
	}

	logger.Info("邮件发送完成",
		"subject", subject,
		"to_count", len(recipients),
		"total_cost_ms", time.Since(start).Milliseconds(),
	)
	return nil
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