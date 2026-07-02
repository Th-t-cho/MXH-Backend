package router

import (
	mainapp "core/app"
	_ "core/docs"
	"core/internal/handler"
	"core/internal/middleware"
	"fmt"
	"strconv"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fiberSwagger "github.com/gofiber/swagger"
)

func Setup() {
	app := fiber.New(fiber.Config{
		BodyLimit: uploadBodyLimit(),
	})

	app.Use(cors.New())
	app.Use(recover.New())

	setupRouter(app)

	port := mainapp.Config("PORT")
	if len(port) == 0 {
		port = mainapp.Config("WEB_PORT")
	}
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

	users := api.Group("/users", middleware.UserAuth)
	users.Get("/search.json", handler.SearchUsers)
	users.Get("/me.json", handler.GetMyProfile)
	users.Patch("/me.json", handler.UpdateMyProfile)
	users.Get("/:id.json", handler.GetUserProfile)

	chat := api.Group("", middleware.UserAuth)
	chat.Post("/conversations/direct.json", handler.CreateDirectConversation)
	chat.Get("/conversations.json", handler.ListConversations)
	chat.Delete("/conversations/:id.json", handler.DeleteConversation)
	chat.Delete("/conversations/:id/purge.json", handler.PurgeConversation)
	chat.Get("/conversations/:id/messages.json", handler.ListMessages)
	chat.Patch("/conversations/:id/messages/:msgId.json", handler.UpdateMessage)
	chat.Delete("/conversations/:id/messages/:msgId.json", handler.DeleteMessage)

	media := api.Group("/media", middleware.UserAuth)
	media.Post("/upload.json", handler.UploadMedia)

	posts := api.Group("", middleware.UserAuth)
	posts.Post("/posts.json", handler.CreatePost)
	posts.Get("/posts.json", handler.ListPosts)
	posts.Get("/posts/:id.json", handler.GetPost)
	posts.Delete("/posts/:id.json", handler.DeletePost)
	posts.Post("/posts/:id/react.json", handler.ReactPost)
	posts.Post("/posts/:id/share.json", handler.SharePost)
	posts.Post("/posts/:id/comments.json", handler.AddComment)
	posts.Get("/posts/:id/comments.json", handler.ListComments)
	posts.Delete("/posts/:id/comments/:cid.json", handler.DeleteComment)
	posts.Post("/posts/:id/comments/:cid/react.json", handler.ReactComment)
}

func uploadBodyLimit() int {
	megabytes, err := strconv.Atoi(mainapp.Config("UPLOAD_MAX_MB"))
	if err != nil || megabytes <= 0 {
		megabytes = 50
	}
	return megabytes * 1024 * 1024
}
