package handler

import (
	"core/app"
	"core/internal/model"
	"core/internal/repo"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
)

func generateOTP() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func otpErrorResponse(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, repo.ErrOTPExpired):
		return c.JSON(fiber.Map{"message": "OTP expired"})
	case errors.Is(err, repo.ErrOTPTooManyAttempts):
		return c.JSON(fiber.Map{"message": "Too many OTP attempts"})
	default:
		return c.JSON(fiber.Map{"message": "Invalid OTP"})
	}
}

func hashOTP(email string, otp string) string {
	key := strings.TrimSpace(app.Config("SECRETKEY"))
	if key == "" {
		key = "default-secret"
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(repo.NormalizeEmail(email)))
	mac.Write([]byte(":"))
	mac.Write([]byte(strings.TrimSpace(otp)))
	return hex.EncodeToString(mac.Sum(nil))
}

func otpTTL() time.Duration {
	minutes, err := strconv.Atoi(app.Config("OTP_TTL_MINUTES"))
	if err != nil || minutes <= 0 {
		minutes = 5
	}
	return time.Duration(minutes) * time.Minute
}

func isValidEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}

func generateRegisterToken(email string) (string, error) {
	claims := jwt.MapClaims{
		"purpose": "register",
		"email":   repo.NormalizeEmail(email),
		"exp":     time.Now().Add(registerTokenTTL()).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret()))
}

func generateAuthTokens(user model.User) (string, string, error) {
	accessToken, err := generateUserToken(user, "access", accessTokenTTL())
	if err != nil {
		return "", "", err
	}

	refreshToken, err := generateUserToken(user, "refresh", refreshTokenTTL())
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func generateUserToken(user model.User, tokenType string, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"type":     tokenType,
		"user_id":  user.ID.String(),
		"email":    user.Email,
		"username": user.Username,
		"role":     user.Role,
		"exp":      time.Now().Add(ttl).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret()))
}

func parseRegisterToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(jwtSecret()), nil
	})
	if err != nil || !token.Valid {
		return "", errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims")
	}

	purpose, _ := claims["purpose"].(string)
	email, _ := claims["email"].(string)
	if purpose != "register" || !isValidEmail(email) {
		return "", errors.New("invalid register token")
	}

	return repo.NormalizeEmail(email), nil
}

func registerTokenFromRequest(c *fiber.Ctx, bodyToken string) string {
	token := strings.TrimSpace(bodyToken)
	if token != "" {
		return token
	}

	authHeader := strings.TrimSpace(c.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

func registerTokenTTL() time.Duration {
	minutes, err := strconv.Atoi(app.Config("REGISTER_TOKEN_TTL_MINUTES"))
	if err != nil || minutes <= 0 {
		minutes = 15
	}
	return time.Duration(minutes) * time.Minute
}

func accessTokenTTL() time.Duration {
	minutes, err := strconv.Atoi(app.Config("ACCESS_TOKEN_TTL_MINUTES"))
	if err != nil || minutes <= 0 {
		minutes = 60
	}
	return time.Duration(minutes) * time.Minute
}

func refreshTokenTTL() time.Duration {
	hours, err := strconv.Atoi(app.Config("REFRESH_TOKEN_TTL_HOURS"))
	if err != nil || hours <= 0 {
		hours = 24 * 30
	}
	return time.Duration(hours) * time.Hour
}

func jwtSecret() string {
	key := strings.TrimSpace(app.Config("SECRETKEY"))
	if key == "" {
		return "default-secret"
	}
	return key
}
