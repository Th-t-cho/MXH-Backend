package middleware

import (
	"core/app"
	"core/internal/repo"
	"strings"

	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v3"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

func Authen(c *fiber.Ctx) error {
	if strings.Contains(c.Path(), "/cms/login.json") {
		return c.Next()
	} else {
		return jwtware.New(jwtware.Config{
			SigningKey:     []byte(jwtSecret()),
			ErrorHandler:   jwtError,
			SuccessHandler: jwtSuccess,
			ContextKey:     "token",
		})(c)
	}

}

func UserAuth(c *fiber.Ctx) error {
	return jwtware.New(jwtware.Config{
		SigningKey:     []byte(jwtSecret()),
		ErrorHandler:   jwtError,
		SuccessHandler: userJWTSuccess,
		ContextKey:     "token",
	})(c)
}

func jwtError(c *fiber.Ctx, err error) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "Invalid or expired JWT"})
}

func userJWTSuccess(c *fiber.Ctx) error {
	claims := c.Locals("token").(*jwt.Token).Claims.(jwt.MapClaims)
	tokenType, _ := claims["type"].(string)
	if tokenType != "access" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "Invalid token type"})
	}

	userId, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "Invalid token payload"})
	}

	user_id, err := uuid.Parse(userId)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "Invalid user id"})
	}

	user, err := repo.GetUserByID(user_id)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "User not found"})
	}

	if user.Status != "active" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"status": false, "message": "User is inactive"})
	}

	c.Locals("user", user)
	return c.Next()
}

func jwtSuccess(c *fiber.Ctx) error {
	claims := c.Locals("token").(*jwt.Token).Claims.(jwt.MapClaims)
	userId, ok := claims["user_id"].(string)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "Invalid token payload"})
	}

	user_id, err := uuid.Parse(userId)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "Invalid user id"})
	}

	user, err := repo.GetUserByID(user_id)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": false, "message": "User not found"})
	}

	if user.Role != "Admin" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"status": false, "message": "Access denied"})
	}
	c.Locals("user", user)
	return c.Next()
}

func jwtSecret() string {
	key := strings.TrimSpace(app.Config("SECRETKEY"))
	if key == "" {
		return "default-secret"
	}
	return key
}
