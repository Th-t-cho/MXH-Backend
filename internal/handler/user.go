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

type messageResponse struct {
	Message string `json:"message"`
}

type verifyRegisterOTPResponse struct {
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
	Message string               `json:"message"`
	Data    registerUserResponse `json:"data"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
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
		return c.JSON(fiber.Map{"message": "Invalid request body"})
	}

	email := repo.NormalizeEmail(req.Email)
	if !isValidEmail(email) {
		return c.JSON(fiber.Map{"message": "Invalid email"})
	}

	if _, err := repo.GetUserByEmail(email); err == nil {
		return c.JSON(fiber.Map{"message": "Email already exists"})
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(fiber.Map{"message": "Failed to check email"})
	}

	otp, err := generateOTP()
	if err != nil {
		return c.JSON(fiber.Map{"message": "Failed to generate OTP"})
	}

	ttl := otpTTL()
	if err := repo.CreateRegisterOTP(email, hashOTP(email, otp), time.Now().Add(ttl)); err != nil {
		return c.JSON(fiber.Map{"message": "Failed to save OTP"})
	}

	if err := service.SendRegisterOTP(email, otp, ttl); err != nil {
		return c.JSON(fiber.Map{"message": "Failed to send OTP"})
	}

	return c.JSON(fiber.Map{"message": "OTP sent"})
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
		return c.JSON(fiber.Map{"message": "Invalid request body"})
	}

	email := repo.NormalizeEmail(req.Email)
	otp := strings.TrimSpace(req.OTP)
	if !isValidEmail(email) || len(otp) != 6 {
		return c.JSON(fiber.Map{"message": "Invalid verify data"})
	}

	if _, err := repo.GetUserByEmail(email); err == nil {
		return c.JSON(fiber.Map{"message": "Email already exists"})
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(fiber.Map{"message": "Failed to check email"})
	}

	if err := repo.VerifyRegisterOTP(email, hashOTP(email, otp), true); err != nil {
		return otpErrorResponse(c, err)
	}

	token, err := generateRegisterToken(email)
	if err != nil {
		return c.JSON(fiber.Map{"message": "Failed to create register token"})
	}

	return c.JSON(fiber.Map{"message": "OTP verified", "token": token})
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
		return c.JSON(fiber.Map{"message": "Invalid request body"})
	}

	username := strings.TrimSpace(req.Username)
	token := registerTokenFromRequest(c, req.Token)

	if username == "" || len(req.Password) < 6 || token == "" {
		return c.JSON(fiber.Map{"message": "Invalid register data"})
	}

	email, err := parseRegisterToken(token)
	if err != nil {
		return c.JSON(fiber.Map{"message": "Invalid or expired register token"})
	}

	if _, err := repo.GetUserByEmail(email); err == nil {
		return c.JSON(fiber.Map{"message": "Email already exists"})
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(fiber.Map{"message": "Failed to check email"})
	}

	if _, err := repo.GetUser(username); err == nil {
		return c.JSON(fiber.Map{"message": "Username already exists"})
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(fiber.Map{"message": "Failed to check username"})
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.JSON(fiber.Map{"message": "Failed to hash password"})
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
		return c.JSON(fiber.Map{"message": "Failed to create user"})
	}

	return c.JSON(fiber.Map{"message": "User created", "data": user})
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
		return c.JSON(fiber.Map{"message": "Invalid request body"})
	}

	account := strings.TrimSpace(req.Account)
	if account == "" || req.Password == "" {
		return c.JSON(fiber.Map{"message": "Invalid login data"})
	}

	user, err := repo.GetUserByAccount(account)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(fiber.Map{"message": "Invalid account or password"})
	}
	if err != nil {
		return c.JSON(fiber.Map{"message": "Failed to get user"})
	}
	if user.Status != "active" {
		return c.JSON(fiber.Map{"message": "User is inactive"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return c.JSON(fiber.Map{"message": "Invalid account or password"})
	}

	accessToken, refreshToken, err := generateAuthTokens(user)
	if err != nil {
		return c.JSON(fiber.Map{"message": "Failed to create tokens"})
	}

	return c.JSON(fiber.Map{
		"status":        "success",
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}
