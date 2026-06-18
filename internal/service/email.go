package service

import (
	"bytes"
	"core/app"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type resendEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

func SendRegisterOTP(email string, otp string, expiresIn time.Duration) error {
	return sendOTPEmail(email, otp, expiresIn, "Register OTP", "Your verification code")
}

func SendForgotPasswordOTP(email string, otp string, expiresIn time.Duration) error {
	return sendOTPEmail(email, otp, expiresIn, "Forgot password OTP", "Your password reset code")
}

func sendOTPEmail(email string, otp string, expiresIn time.Duration, logLabel string, subject string) error {
	provider := strings.ToLower(strings.TrimSpace(app.Config("EMAIL_PROVIDER")))
	if provider == "" {
		provider = "console"
	}

	maskedEmail := maskEmail(email)
	logrus.WithFields(logrus.Fields{
		"provider": provider,
		"email":    maskedEmail,
		"subject":  subject,
	}).Info("Sending OTP email")

	var err error
	switch provider {
	case "console":
		logrus.Infof("%s for %s: %s (expires in %s)", logLabel, maskedEmail, otp, expiresIn.String())
		return nil
	case "resend":
		err = sendOTPWithResend(email, otp, expiresIn, subject)
	case "smtp":
		err = sendOTPWithSMTP(email, otp, expiresIn, subject)
	default:
		err = fmt.Errorf("unsupported EMAIL_PROVIDER: %s", provider)
	}
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"provider": provider,
			"email":    maskedEmail,
			"error":    err.Error(),
		}).Error("Failed to send OTP email")
		return err
	}

	logrus.WithFields(logrus.Fields{
		"provider": provider,
		"email":    maskedEmail,
	}).Info("OTP email sent")
	return nil
}

func sendOTPWithResend(email string, otp string, expiresIn time.Duration, subject string) error {
	apiKey := strings.TrimSpace(app.Config("RESEND_API_KEY"))
	from := strings.TrimSpace(app.Config("MAIL_FROM"))
	if apiKey == "" {
		return errors.New("RESEND_API_KEY is required")
	}
	if from == "" {
		return errors.New("MAIL_FROM is required")
	}

	body := resendEmailRequest{
		From:    from,
		To:      []string{email},
		Subject: subject,
		HTML: fmt.Sprintf(
			`<p>%s is <strong>%s</strong>.</p><p>This code expires in %d minutes.</p>`,
			subject,
			otp,
			int(expiresIn.Minutes()),
		),
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("resend returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func sendOTPWithSMTP(email string, otp string, expiresIn time.Duration, subject string) error {
	host := strings.TrimSpace(app.Config("SMTP_HOST"))
	port := strings.TrimSpace(app.Config("SMTP_PORT"))
	username := strings.TrimSpace(app.Config("SMTP_USERNAME"))
	password := strings.ReplaceAll(strings.TrimSpace(app.Config("SMTP_PASSWORD")), " ", "")
	from := strings.TrimSpace(app.Config("MAIL_FROM"))

	if host == "" {
		return errors.New("SMTP_HOST is required")
	}
	if port == "" {
		port = "587"
	}
	if username == "" {
		return errors.New("SMTP_USERNAME is required")
	}
	if password == "" {
		return errors.New("SMTP_PASSWORD is required")
	}
	if from == "" {
		from = username
	}
	logrus.WithFields(logrus.Fields{
		"host":          host,
		"port":          port,
		"username_set":  username != "",
		"password_set":  password != "",
		"mail_from_set": from != "",
		"timeout":       smtpTimeout().String(),
	}).Info("SMTP config loaded")

	fromAddress := from
	parsedFrom, err := mail.ParseAddress(from)
	if err == nil {
		fromAddress = parsedFrom.Address
	}

	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber <= 0 {
		return errors.New("SMTP_PORT is invalid")
	}

	html := fmt.Sprintf(
		`<p>%s is <strong>%s</strong>.</p><p>This code expires in %d minutes.</p>`,
		subject,
		otp,
		int(expiresIn.Minutes()),
	)
	message := strings.Join([]string{
		"From: " + from,
		"To: " + email,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		html,
	}, "\r\n")

	auth := smtp.PlainAuth("", username, password, host)
	return sendSMTPMailWithTimeout(
		fmt.Sprintf("%s:%d", host, portNumber),
		host,
		auth,
		fromAddress,
		[]string{email},
		[]byte(message),
	)
}

func sendSMTPMailWithTimeout(addr string, host string, auth smtp.Auth, from string, to []string, message []byte) error {
	timeout := smtpTimeout()
	dialer := net.Dialer{Timeout: timeout}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Hello("localhost"); err != nil {
		return err
	}

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtp auth is not supported")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	return client.Quit()
}

func smtpTimeout() time.Duration {
	seconds, err := strconv.Atoi(app.Config("SMTP_TIMEOUT_SECONDS"))
	if err != nil || seconds <= 0 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}

	name := parts[0]
	domain := parts[1]
	if len(name) <= 2 {
		return "***@" + domain
	}
	return name[:2] + "***@" + domain
}
