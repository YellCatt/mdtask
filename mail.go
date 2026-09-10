package main

import (
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// east8 报告用的时区；没有 tzdata 就退化成固定 UTC+8
var east8 = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()

// ---------- 本机 IP ----------

type IPInfo struct {
	Address   string
	IsGateway bool
}

// localIPs 收集已启用、非回环网卡上的 IPv4 / IPv6
func localIPs() (ipv4, ipv6 []IPInfo) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			s := ip.String()
			if s == "" || s[0] == ':' {
				continue
			}
			if ip.To4() != nil {
				ipv4 = append(ipv4, IPInfo{Address: s, IsGateway: strings.HasSuffix(s, ".1")})
			} else {
				ipv6 = append(ipv6, IPInfo{Address: s})
			}
		}
	}
	return ipv4, ipv6
}

// findInternetIP 挨个地址往外拨一下，能拨通的那个就是真正能上外网的地址
func findInternetIP(ipv4List []IPInfo) string {
	targets := []string{"8.8.8.8:53", "223.5.5.5:53", "1.1.1.1:53", "114.114.114.114:53"}
	for _, info := range ipv4List {
		ip := net.ParseIP(info.Address)
		if ip == nil || ip.To4() == nil {
			continue
		}
		localAddr := &net.TCPAddr{IP: ip, Port: 0}
		for _, target := range targets {
			dialer := &net.Dialer{LocalAddr: localAddr, Timeout: 2 * time.Second}
			conn, err := dialer.Dial("tcp", target)
			if err == nil {
				conn.Close()
				return info.Address
			}
		}
	}
	return ""
}

func formatIPList(list []IPInfo, internetIP string) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	for _, info := range list {
		line := "- " + info.Address
		if info.IsGateway {
			line += " (网关地址)"
		}
		if info.Address == internetIP {
			line += " ←(访问外网地址)"
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

func hostName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "未知设备"
}

// buildIPReport 设备名 + IPv4/IPv6 清单，邮件正文就这段
func buildIPReport() string {
	now := time.Now().In(east8)
	ipv4, ipv6 := localIPs()
	netIP := findInternetIP(ipv4)

	var b strings.Builder
	fmt.Fprintf(&b, "设备名称：%s\n\n", hostName())

	b.WriteString("【IPv4 地址】\n")
	if s := formatIPList(ipv4, netIP); s != "" {
		b.WriteString(s)
	} else {
		b.WriteString("未找到 IPv4 地址\n")
	}
	b.WriteString("\n【IPv6 地址】\n")
	if s := formatIPList(ipv6, ""); s != "" {
		b.WriteString(s)
	} else {
		b.WriteString("未找到 IPv6 地址\n")
	}
	fmt.Fprintf(&b, "\n发送时间：%s\n来自 MDTask\n", now.Format("2006年01月02日 15:04:05"))
	return b.String()
}

// ---------- 发信 ----------

// mimeB 中文标题要按 RFC2047 编码，否则客户端里是乱码
func mimeB(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var parts []string
	for i := 0; i < len(s); i += 18 { // 每 18 字节一段，避免单行过长
		j := i + 18
		if j > len(s) {
			j = len(s)
		}
		parts = append(parts, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(s[i:j]))+"?=")
	}
	return strings.Join(parts, "\r\n ")
}

func domainOf(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 {
		return email[i+1:]
	}
	return "localhost"
}

func buildMessage(from string, to []string, subject, body string, now time.Time) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mimeB(subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%d.mdtask@%s>\r\n", now.UnixNano(), domainOf(from))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return []byte(b.String())
}

// sendMailTLS 直连 TLS（465 端口那类）发信
func sendMailTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte, skipVerify bool, timeout time.Duration) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: skipVerify, ServerName: host}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout * 3))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()

	if auth != nil {
		if err = c.Auth(auth); err != nil {
			return err
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	for _, a := range to {
		if err = c.Rcpt(a); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(msg); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// splitEmails 逗号 / 分号 / 空格都能分隔
func splitEmails(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---------- 命令 ----------

func cmdMail(args []string) {
	fs := flag.NewFlagSet("mail", flag.ExitOnError)
	to := fs.String("to", "", "收件人，多个用逗号分隔，默认取配置 mail.to_email")
	subject := fs.String("subject", "", "邮件标题，默认自动生成")
	extra := fs.String("body", "", "附加在 IP 信息后面的正文")
	dry := fs.Bool("dry", false, "只打印邮件内容，不真的发")
	fs.Parse(args)

	c := cfg.Mail
	recipients := splitEmails(*to)
	if len(recipients) == 0 {
		recipients = splitEmails(c.ToEmail)
	}
	if len(recipients) == 0 {
		fatal("没有收件人：在 config.yaml 的 mail.to_email 里填，或用 -to 指定")
	}
	if strings.TrimSpace(c.SMTPHost) == "" {
		fatal("没配 SMTP 服务器：config.yaml 的 mail.smtp_host")
	}
	if strings.TrimSpace(c.FromEmail) == "" {
		fatal("没配发件邮箱：config.yaml 的 mail.from_email")
	}
	if c.AuthCode == "" {
		fatal("没配邮箱授权码：config.yaml 的 mail.auth_code（不是登录密码）")
	}
	port := c.SMTPPort
	if port <= 0 {
		port = 465
	}
	timeout := time.Duration(c.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	now := time.Now().In(east8)
	sub := strings.TrimSpace(*subject)
	if sub == "" {
		sub = fmt.Sprintf("[%s] 本地 IP 地址信息 %s", hostName(), now.Format("2006-01-02 15:04"))
	}
	body := buildIPReport()
	if s := strings.TrimSpace(*extra); s != "" {
		body += "\n" + s + "\n"
	}
	msg := buildMessage(c.FromEmail, recipients, sub, body, now)

	if *dry {
		fmt.Println(paint(cBold, "收件人: ") + strings.Join(recipients, ", "))
		fmt.Println(paint(cBold, "标　题: ") + sub)
		fmt.Println(paint(cDim, "────────── 正文 ──────────"))
		fmt.Print(body)
		return
	}

	fmt.Printf("连接 %s:%d ...\n", c.SMTPHost, port)
	if c.TLSSkipVerify {
		fmt.Println(paint(cYel, "⚠️ 已跳过 TLS 证书校验"))
	}
	auth := smtp.PlainAuth("", c.FromEmail, c.AuthCode, c.SMTPHost)
	addr := fmt.Sprintf("%s:%d", c.SMTPHost, port)
	if err := sendMailTLS(addr, auth, c.FromEmail, recipients, msg, c.TLSSkipVerify, timeout); err != nil {
		fatal(fmt.Errorf("发送失败: %w", err))
	}
	fmt.Println(paint(cGreen, "✅ 已发送到 " + strings.Join(recipients, ", ")))
}
