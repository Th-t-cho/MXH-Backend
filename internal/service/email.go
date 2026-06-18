package service

import (
	"bytes"
	"core/app"
	"encoding/json"
	"errors"
	"fmt"
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

	switch provider {
	case "console":
		logrus.Infof("%s for %s: %s (expires in %s)", logLabel, email, otp, expiresIn.String())
		return nil
	case "resend":
		return sendOTPWithResend(email, otp, expiresIn, subject)
	case "smtp":
		return sendOTPWithSMTP(email, otp, expiresIn, subject)
	default:
		return fmt.Errorf("unsupported EMAIL_PROVIDER: %s", provider)
	}
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
		return fmt.Errorf("resend returned status %d", resp.StatusCode)
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
	return smtp.SendMail(
		fmt.Sprintf("%s:%d", host, portNumber),
		auth,
		fromAddress,
		[]string{email},
		[]byte(message),
	)
}
