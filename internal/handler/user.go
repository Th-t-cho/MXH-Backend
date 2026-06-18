package handler

import (
	"core/internal/model"
	"core/internal/repo"
	"core/internal/service"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type sendRegisterOTPRequest struct {
	Email string `json:"email"`
}

type verifyRegisterOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type forgotPasswordOTPRequest struct {
	Email string `json:"email"`
}

type verifyForgotPasswordOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type messageResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
}

type verifyRegisterOTPResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Token   string `json:"token"`
}

type registerUserResponse struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	IsVerify bool   `json:"is_verify"`
}

type registerResponse struct {
	Status  bool                 `json:"status"`
	Message string               `json:"message"`
	Data    registerUserResponse `json:"data"`
}

type loginResponse struct {
	Status       bool   `json:"status"`
	Message      string `json:"message"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type forgotPasswordVerifyResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Token   string `json:"token"`
}

// SendRegisterOTP sends an OTP to an email before registration.
// @Summary Send register OTP
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body sendRegisterOTPRequest true "Email request"
// @Success 200 {object} messageResponse
// @Router /api/auth/register/otp.json [post]
func SendRegisterOTP(c *fiber.Ctx) error {
	req := sendRegisterOTPRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	email := repo.NormalizeEmail(req.Email)
	if !isValidEmail(email) {
		return c.JSON(errorResponse("Invalid email"))
	}

	if _, err := repo.GetUserByEmail(email); err == nil {
		return c.JSON(errorResponse("Email already exists"))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("Failed to check email"))
	}

	otp, err := generateOTP()
	if err != nil {
		return c.JSON(errorResponse("Failed to generate OTP"))
	}

	ttl := otpTTL()
	if err := repo.CreateRegisterOTP(email, hashOTP(email, otp), time.Now().Add(ttl)); err != nil {
		return c.JSON(errorResponse("Failed to save OTP"))
	}

	if err := service.SendRegisterOTP(email, otp, ttl); err != nil {
		return c.JSON(errorResponse("Failed to send OTP"))
	}

	return c.JSON(successResponse("OTP sent"))
}

// VerifyRegisterOTP verifies an email OTP and returns a temporary register token.
// @Summary Verify register OTP
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body verifyRegisterOTPRequest true "OTP verify request"
// @Success 200 {object} verifyRegisterOTPResponse
// @Router /api/auth/register/otp/verify.json [post]
func VerifyRegisterOTP(c *fiber.Ctx) error {
	req := verifyRegisterOTPRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	email := repo.NormalizeEmail(req.Email)
	otp := strings.TrimSpace(req.OTP)
	if !isValidEmail(email) || len(otp) != 6 {
		return c.JSON(errorResponse("Invalid verify data"))
	}

	if _, err := repo.GetUserByEmail(email); err == nil {
		return c.JSON(errorResponse("Email already exists"))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("Failed to check email"))
	}

	if err := repo.VerifyRegisterOTP(email, hashOTP(email, otp), true); err != nil {
		return otpErrorResponse(c, err)
	}

	token, err := generateRegisterToken(email)
	if err != nil {
		return c.JSON(errorResponse("Failed to create register token"))
	}

	return c.JSON(successResponse("OTP verified", fiber.Map{"token": token}))
}

// Register creates a user using the token returned after OTP verification.
// @Summary Register user
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body registerRequest true "Register request"
// @Success 200 {object} registerResponse
// @Router /api/auth/register.json [post]
func Register(c *fiber.Ctx) error {
	req := registerRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	username := strings.TrimSpace(req.Username)
	token := registerTokenFromRequest(c, req.Token)

	if username == "" || len(req.Password) < 6 || token == "" {
		return c.JSON(errorResponse("Invalid register data"))
	}

	email, err := parseRegisterToken(token)
	if err != nil {
		return c.JSON(errorResponse("Invalid or expired register token"))
	}

	if _, err := repo.GetUserByEmail(email); err == nil {
		return c.JSON(errorResponse("Email already exists"))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("Failed to check email"))
	}

	if _, err := repo.GetUser(username); err == nil {
		return c.JSON(errorResponse("Username already exists"))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("Failed to check username"))
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.JSON(errorResponse("Failed to hash password"))
	}

	user := model.User{
		Email:    email,
		Username: username,
		Password: string(hashedPassword),
		Role:     "User",
		Status:   "active",
		IsVerify: true,
	}

	if err := repo.CreateUser(&user); err != nil {
		return c.JSON(errorResponse("Failed to create user"))
	}

	return c.JSON(successResponse("User created", fiber.Map{"data": user}))
}

// Login authenticates a user and returns access and refresh tokens.
// @Summary Login user
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body loginRequest true "Login request"
// @Success 200 {object} loginResponse
// @Router /api/auth/login.json [post]
func Login(c *fiber.Ctx) error {
	req := loginRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	account := strings.TrimSpace(req.Account)
	if account == "" || req.Password == "" {
		return c.JSON(errorResponse("Invalid login data"))
	}

	user, err := repo.GetUserByAccount(account)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("Invalid account or password"))
	}
	if err != nil {
		return c.JSON(errorResponse("Failed to get user"))
	}
	if user.Status != "active" {
		return c.JSON(errorResponse("User is inactive"))
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return c.JSON(errorResponse("Invalid account or password"))
	}

	accessToken, refreshToken, err := generateAuthTokens(user)
	if err != nil {
		return c.JSON(errorResponse("Failed to create tokens"))
	}

	return c.JSON(successResponse("Login successfully", fiber.Map{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	}))
}

// SendForgotPasswordOTP sends an OTP to reset password.
// @Summary Send forgot password OTP
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body forgotPasswordOTPRequest true "Forgot password OTP request"
// @Success 200 {object} messageResponse
// @Router /api/auth/forgot-password/otp.json [post]
func SendForgotPasswordOTP(c *fiber.Ctx) error {
	req := forgotPasswordOTPRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	email := repo.NormalizeEmail(req.Email)
	if !isValidEmail(email) {
		return c.JSON(errorResponse("Invalid email"))
	}

	if _, err := repo.GetUserByEmail(email); errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(successResponse("If the email exists, an OTP will be sent"))
	} else if err != nil {
		return c.JSON(errorResponse("Failed to check email"))
	}

	otp, err := generateOTP()
	if err != nil {
		return c.JSON(errorResponse("Failed to generate OTP"))
	}

	ttl := otpTTL()
	if err := repo.CreatePasswordResetOTP(email, hashOTP(email, otp), time.Now().Add(ttl)); err != nil {
		return c.JSON(errorResponse("Failed to save OTP"))
	}

	if err := service.SendForgotPasswordOTP(email, otp, ttl); err != nil {
		return c.JSON(errorResponse("Failed to send OTP"))
	}

	return c.JSON(successResponse("If the email exists, an OTP will be sent"))
}

// VerifyForgotPasswordOTP verifies a password reset OTP and returns a reset token.
// @Summary Verify forgot password OTP
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body verifyForgotPasswordOTPRequest true "Forgot password OTP verify request"
// @Success 200 {object} forgotPasswordVerifyResponse
// @Router /api/auth/forgot-password/otp/verify.json [post]
func VerifyForgotPasswordOTP(c *fiber.Ctx) error {
	req := verifyForgotPasswordOTPRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	email := repo.NormalizeEmail(req.Email)
	otp := strings.TrimSpace(req.OTP)
	if !isValidEmail(email) || len(otp) != 6 {
		return c.JSON(errorResponse("Invalid verify data"))
	}

	if _, err := repo.GetUserByEmail(email); errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("Invalid OTP"))
	} else if err != nil {
		return c.JSON(errorResponse("Failed to check email"))
	}

	if err := repo.VerifyPasswordResetOTP(email, hashOTP(email, otp), true); err != nil {
		return otpErrorResponse(c, err)
	}

	token, err := generatePasswordResetToken(email)
	if err != nil {
		return c.JSON(errorResponse("Failed to create reset token"))
	}

	return c.JSON(successResponse("OTP verified", fiber.Map{"token": token}))
}

// ResetPassword resets a user password using the token returned after OTP verification.
// @Summary Reset password
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body resetPasswordRequest true "Reset password request"
// @Success 200 {object} messageResponse
// @Router /api/auth/forgot-password/reset.json [post]
func ResetPassword(c *fiber.Ctx) error {
	req := resetPasswordRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	token := passwordResetTokenFromRequest(c, req.Token)
	if token == "" || len(req.NewPassword) < 6 {
		return c.JSON(errorResponse("Invalid reset data"))
	}

	email, err := parsePasswordResetToken(token)
	if err != nil {
		return c.JSON(errorResponse("Invalid or expired reset token"))
	}

	user, err := repo.GetUserByEmail(email)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("User not found"))
	}
	if err != nil {
		return c.JSON(errorResponse("Failed to get user"))
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return c.JSON(errorResponse("Failed to hash password"))
	}

	if err := repo.UpdateUserPassword(user.ID, string(hashedPassword)); err != nil {
		return c.JSON(errorResponse("Failed to reset password"))
	}

	return c.JSON(successResponse("Password reset successfully"))
}
