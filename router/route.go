package router

import (
	mainapp "core/app"
	_ "core/docs"
	"core/internal/handler"
	"fmt"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fiberSwagger "github.com/gofiber/swagger"
)

func Setup() {
	app := fiber.New(fiber.Config{
		BodyLimit: 6 * 1024 * 1024, // Limit request body size to 6MB (accommodates 5MB files).
	})

	app.Use(cors.New())
	app.Use(recover.New())

	setupRouter(app)

	port := mainapp.Config("WEB_PORT")
	if len(port) == 0 {
		port = "6969"
	}

	fmt.Println("Server is running on port=", port)
	app.Listen(":" + port)
}

func setupRouter(fiber_app *fiber.App) {
	fiber_app.Get("/swagger/*", fiberSwagger.New())
	fiber_app.Get("/ws/chat", websocket.New(handler.ChatWebSocket))

	api := fiber_app.Group("/api", logger.New())

	api.Get("/api.json", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": true, "message": "Pong"})
	})

	api.Post("/auth/register/otp.json", handler.SendRegisterOTP)
	api.Post("/auth/register/otp/verify.json", handler.VerifyRegisterOTP)
	api.Post("/auth/register.json", handler.Register)
	api.Post("/auth/login.json", handler.Login)
	api.Post("/auth/forgot-password/otp.json", handler.SendForgotPasswordOTP)
	api.Post("/auth/forgot-password/otp/verify.json", handler.VerifyForgotPasswordOTP)
	api.Post("/auth/forgot-password/reset.json", handler.ResetPassword)

	api.Post("/conversations/direct.json", handler.CreateDirectConversation)
	api.Get("/conversations.json", handler.ListConversations)
	api.Get("/conversations/:id/messages.json", handler.ListMessages)
}
