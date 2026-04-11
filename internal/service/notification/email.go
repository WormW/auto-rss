package notification

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/WormW/auto-rss/internal/pkg/logger"
)

const (
	emailTimeout = 30 * time.Second
)

// EmailConfig SMTP 邮件配置
type EmailConfig struct {
	SMTPHost     string `json:"smtp_host"`     // SMTP 服务器地址，如 smtp.gmail.com
	SMTPPort     int    `json:"smtp_port"`     // SMTP 端口，如 587, 465
	Username     string `json:"username"`      // 邮箱账号
	Password     string `json:"password"`      // 邮箱密码或授权码
	From         string `json:"from"`          // 发件人显示名称
	To           string `json:"to"`            // 收件人邮箱（多个用逗号分隔）
	UseTLS       bool   `json:"use_tls"`       // 是否使用 TLS（端口 465）
	UseStartTLS  bool   `json:"use_starttls"`  // 是否使用 STARTTLS（端口 587）
}

// EmailChannel 邮件通知渠道
type EmailChannel struct {
	config  *EmailConfig
	enabled bool
}

// NewEmailChannel 创建邮件通知渠道
func NewEmailChannel(config *EmailConfig) *EmailChannel {
	return &EmailChannel{
		config:  config,
		enabled: config.SMTPHost != "" && config.Username != "" && config.Password != "" && config.To != "",
	}
}

// Name 返回渠道名称
func (e *EmailChannel) Name() string {
	return "email"
}

// IsEnabled 返回渠道是否启用
func (e *EmailChannel) IsEnabled() bool {
	return e.enabled
}

// Send 发送邮件通知
func (e *EmailChannel) Send(title, message string) error {
	if !e.enabled {
		return fmt.Errorf("email channel not enabled")
	}

	// 解析收件人列表
	toList := strings.Split(e.config.To, ",")
	for i := range toList {
		toList[i] = strings.TrimSpace(toList[i])
	}

	// 构建邮件内容
	from := e.config.From
	if from == "" {
		from = e.config.Username
	}

	subject := title
	body := message

	// 构建邮件头
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("%s <%s>", from, e.config.Username)
	headers["To"] = strings.Join(toList, ", ")
	headers["Subject"] = subject
	headers["Content-Type"] = "text/plain; charset=UTF-8"
	headers["Date"] = time.Now().Format(time.RFC1123)

	// 构建完整邮件内容
	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// 发送邮件
	if e.config.UseTLS {
		return e.sendViaTLS(toList, msg.String())
	}
	return e.sendViaStartTLS(toList, msg.String())
}

// sendViaStartTLS 使用 STARTTLS 发送邮件（端口 587）
func (e *EmailChannel) sendViaStartTLS(toList []string, msg string) error {
	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)

	// 连接到 SMTP 服务器
	conn, err := net.DialTimeout("tcp", addr, emailTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	// 创建 SMTP 客户端
	client, err := smtp.NewClient(conn, e.config.SMTPHost)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// 如果使用 STARTTLS
	if e.config.UseStartTLS {
		tlsConfig := &tls.Config{
			ServerName: e.config.SMTPHost,
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to start TLS: %w", err)
		}
	}

	// 认证
	auth := smtp.PlainAuth("", e.config.Username, e.config.Password, e.config.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	// 设置发件人
	if err := client.Mail(e.config.Username); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// 设置收件人
	for _, to := range toList {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", to, err)
		}
	}

	// 发送邮件内容
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}

	_, err = w.Write([]byte(msg))
	if err != nil {
		w.Close()
		return fmt.Errorf("failed to write message: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	// 退出
	if err := client.Quit(); err != nil {
		logger.Warn("SMTP quit failed", "error", err)
	}

	logger.Debug("Email notification sent successfully",
		"to", strings.Join(toList, ", "))

	return nil
}

// sendViaTLS 使用 TLS 发送邮件（端口 465）
func (e *EmailChannel) sendViaTLS(toList []string, msg string) error {
	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)

	// 创建 TLS 配置
	tlsConfig := &tls.Config{
		ServerName: e.config.SMTPHost,
	}

	// 连接到 SMTP 服务器（TLS）
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server via TLS: %w", err)
	}
	defer conn.Close()

	// 创建 SMTP 客户端
	client, err := smtp.NewClient(conn, e.config.SMTPHost)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// 认证
	auth := smtp.PlainAuth("", e.config.Username, e.config.Password, e.config.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	// 设置发件人
	if err := client.Mail(e.config.Username); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// 设置收件人
	for _, to := range toList {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", to, err)
		}
	}

	// 发送邮件内容
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}

	_, err = w.Write([]byte(msg))
	if err != nil {
		w.Close()
		return fmt.Errorf("failed to write message: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	// 退出
	if err := client.Quit(); err != nil {
		logger.Warn("SMTP quit failed", "error", err)
	}

	logger.Debug("Email notification sent successfully via TLS",
		"to", strings.Join(toList, ", "))

	return nil
}

// TestEmailConfig 测试邮件配置
func TestEmailConfig(config *EmailConfig) error {
	channel := NewEmailChannel(config)
	if !channel.IsEnabled() {
		return fmt.Errorf("email config incomplete: smtp_host, username, password, and to are required")
	}

	// 发送测试邮件
	return channel.Send(
		"Auto RSS 邮件通知测试",
		"✅ 配置成功！\n\n您已成功启用邮件通知功能。\n\n此邮件由 Auto RSS 自动发送。",
	)
}

// ValidateEmailConfig 验证邮件配置是否有效
func ValidateEmailConfig(config *EmailConfig) error {
	if config.SMTPHost == "" {
		return fmt.Errorf("SMTP host is required")
	}
	if config.SMTPPort == 0 {
		return fmt.Errorf("SMTP port is required")
	}
	if config.Username == "" {
		return fmt.Errorf("username is required")
	}
	if config.Password == "" {
		return fmt.Errorf("password is required")
	}
	if config.To == "" {
		return fmt.Errorf("recipient (to) is required")
	}

	// 验证收件人邮箱格式
	toList := strings.Split(config.To, ",")
	for _, to := range toList {
		to = strings.TrimSpace(to)
		if !isValidEmail(to) {
			return fmt.Errorf("invalid email address: %s", to)
		}
	}

	return nil
}

// isValidEmail 验证邮箱格式
func isValidEmail(email string) bool {
	// 简单验证：包含 @ 和 .
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return false
	}
	// 检查 @ 前后都有内容
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	return true
}

// GetCommonSMTPConfigs 获取常见邮箱 SMTP 配置模板
func GetCommonSMTPConfigs() map[string]*EmailConfig {
	return map[string]*EmailConfig{
		"gmail": {
			SMTPHost:    "smtp.gmail.com",
			SMTPPort:    587,
			UseStartTLS: true,
		},
		"qq": {
			SMTPHost:    "smtp.qq.com",
			SMTPPort:    587,
			UseStartTLS: true,
		},
		"163": {
			SMTPHost:    "smtp.163.com",
			SMTPPort:    465,
			UseTLS:      true,
		},
		"outlook": {
			SMTPHost:    "smtp.office365.com",
			SMTPPort:    587,
			UseStartTLS: true,
		},
	}
}
