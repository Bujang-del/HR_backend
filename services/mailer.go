package services

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

type SMTPMailer struct {
	host     string
	port     string
	username string
	password string
	from     string
}

func NewSMTPMailer(host, port, username, password, from string) *SMTPMailer {
	if from == "" {
		from = username
	}
	return &SMTPMailer{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
	}
}

func (mailer *SMTPMailer) SendInvitation(ctx context.Context, recipient, recipientName, actionLink string) error {
	if mailer.host == "" || mailer.username == "" || mailer.password == "" || mailer.from == "" {
		return fmt.Errorf("SMTP belum dikonfigurasi: SMTP_HOST, SMTP_USERNAME, SMTP_PASSWORD, dan SMTP_FROM wajib diisi")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	greeting := "Halo,"
	if strings.TrimSpace(recipientName) != "" {
		greeting = "Halo " + strings.TrimSpace(recipientName) + ","
	}

	auth := smtp.PlainAuth("", mailer.username, mailer.password, mailer.host)
	message := strings.Join([]string{
		"From: " + mailer.from,
		"To: " + recipient,
		"Subject: Anda diundang ke Gateway Mobile",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		greeting,
		"",
		"HR telah membuat akun Gateway Mobile untuk Anda.",
		"Buka link berikut untuk mengatur password dan mengaktifkan akun:",
		actionLink,
		"",
		"Jika Anda tidak mengenali undangan ini, abaikan email ini.",
	}, "\r\n")
	return smtp.SendMail(mailer.host+":"+mailer.port, auth, envelopeAddress(mailer.from), []string{recipient}, []byte(message))
}

func envelopeAddress(from string) string {
	start := strings.LastIndex(from, "<")
	end := strings.LastIndex(from, ">")
	if start >= 0 && end > start {
		return strings.TrimSpace(from[start+1 : end])
	}
	return strings.TrimSpace(from)
}
