package service

import (
	"bytes"
	"core/app"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type brevoEmailAddress struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

type brevoEmailRequest struct {
	Sender      brevoEmailAddress   `json:"sender"`
	To          []brevoEmailAddress `json:"to"`
	Subject     string              `json:"subject"`
	HTMLContent string              `json:"htmlContent"`
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

	if provider == "console" {
		logrus.Infof("%s for %s: %s (expires in %s)", logLabel, maskedEmail, otp, expiresIn.String())
		return nil
	}

	if provider != "brevo" {
		return fmt.Errorf("unsupported EMAIL_PROVIDER: %s", provider)
	}

	if err := sendOTPWithBrevo(email, otp, expiresIn, subject); err != nil {
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

func sendOTPWithBrevo(email string, otp string, expiresIn time.Duration, subject string) error {
	apiKey := strings.TrimSpace(app.Config("BREVO_API_KEY"))
	from := strings.TrimSpace(app.Config("MAIL_FROM"))
	if apiKey == "" {
		return errors.New("BREVO_API_KEY is required")
	}
	if from == "" {
		return errors.New("MAIL_FROM is required")
	}

	sender, err := parseEmailAddress(from)
	if err != nil {
		return err
	}

	body := brevoEmailRequest{
		Sender: sender,
		To: []brevoEmailAddress{
			{Email: email},
		},
		Subject: subject,
		HTMLContent: fmt.Sprintf(
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

	req, err := http.NewRequest(http.MethodPost, "https://api.brevo.com/v3/smtp/email", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("brevo returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
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

func parseEmailAddress(value string) (brevoEmailAddress, error) {
	parsed, err := mail.ParseAddress(value)
	if err != nil {
		return brevoEmailAddress{}, err
	}
	return brevoEmailAddress{
		Name:  parsed.Name,
		Email: parsed.Address,
	}, nil
}
