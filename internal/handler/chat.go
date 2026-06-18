package handler

import (
	"core/internal/repo"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type createDirectConversationRequest struct {
	UserID uuid.UUID `json:"user_id"`
}

type conversationResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type messageListResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// CreateDirectConversation creates or returns an existing direct conversation.
// @Summary Create direct conversation
// @Tags Chat
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param request body createDirectConversationRequest true "Direct conversation request"
// @Success 200 {object} conversationResponse
// @Router /api/conversations/direct.json [post]
func CreateDirectConversation(c *fiber.Ctx) error {
	user, err := currentUserFromRequest(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	req := createDirectConversationRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	conversation, err := repo.GetOrCreateDirectConversation(user.ID, req.UserID)
	if errors.Is(err, repo.ErrInvalidConversationMember) {
		return c.JSON(errorResponse("Invalid conversation member"))
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("User not found"))
	}
	if err != nil {
		return c.JSON(errorResponse("Failed to create conversation"))
	}

	return c.JSON(successResponse("Conversation ready", fiber.Map{"data": conversation}))
}

// ListConversations returns conversations for the current user.
// @Summary List conversations
// @Tags Chat
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Success 200 {object} conversationResponse
// @Router /api/conversations.json [get]
func ListConversations(c *fiber.Ctx) error {
	user, err := currentUserFromRequest(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	conversations, err := repo.ListConversations(user.ID)
	if err != nil {
		return c.JSON(errorResponse("Failed to list conversations"))
	}

	return c.JSON(successResponse("Conversations fetched", fiber.Map{"data": conversations}))
}

// ListMessages returns messages in a conversation.
// @Summary List conversation messages
// @Tags Chat
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Conversation ID"
// @Param page query int false "Page"
// @Param limit query int false "Limit"
// @Success 200 {object} messageListResponse
// @Router /api/conversations/{id}/messages.json [get]
func ListMessages(c *fiber.Ctx) error {
	user, err := currentUserFromRequest(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	conversationID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid conversation id"))
	}

	query := repo.Query{}
	query.Parse(c)

	messages, err := repo.ListMessages(conversationID, user.ID, query)
	if errors.Is(err, repo.ErrConversationForbidden) {
		return c.Status(fiber.StatusForbidden).JSON(errorResponse("Access denied"))
	}
	if err != nil {
		return c.JSON(errorResponse("Failed to list messages"))
	}

	return c.JSON(successResponse("Messages fetched", fiber.Map{"data": messages}))
}
