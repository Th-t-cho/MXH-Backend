package handler

import (
	"core/internal/repo"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type updateMessageRequest struct {
	Content string `json:"content"`
}

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
	user, err := currentUserFromContext(c)
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
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	conversations, err := repo.ListConversations(user.ID)
	if err != nil {
		return c.JSON(errorResponse("Failed to list conversations"))
	}

	return c.JSON(successResponse("Conversations fetched", fiber.Map{"data": conversations}))
}

// UpdateMessage edits the content of a message (sender only).
// @Summary Edit a message
// @Tags Chat
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Conversation ID"
// @Param msgId path string true "Message ID"
// @Param request body updateMessageRequest true "New content"
// @Success 200 {object} messageListResponse
// @Router /api/conversations/{id}/messages/{msgId}.json [patch]
func UpdateMessage(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	messageID, err := uuid.Parse(c.Params("msgId"))
	if err != nil {
		return c.JSON(errorResponse("Invalid message id"))
	}

	conversationID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid conversation id"))
	}

	req := updateMessageRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	message, err := repo.UpdateMessage(messageID, user.ID, req.Content)
	if errors.Is(err, repo.ErrMessageNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse("Message not found"))
	}
	if errors.Is(err, repo.ErrMessageForbidden) {
		return c.Status(fiber.StatusForbidden).JSON(errorResponse("You can only edit your own messages"))
	}
	if errors.Is(err, gorm.ErrInvalidData) {
		return c.JSON(errorResponse("Content cannot be empty"))
	}
	if err != nil {
		return c.JSON(errorResponse("Failed to update message"))
	}

	go func() {
		userIDs, err := repo.GetConversationMemberIDs(conversationID)
		if err != nil {
			return
		}
		chatHub.sendToUsers(userIDs, chatWSEvent{
			Type:           "message_updated",
			ConversationID: conversationID,
			Data:           message,
		})
	}()

	return c.JSON(successResponse("Message updated", fiber.Map{"data": message}))
}

// DeleteMessage removes a message (sender only).
// @Summary Delete a message
// @Tags Chat
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Conversation ID"
// @Param msgId path string true "Message ID"
// @Success 200 {object} messageListResponse
// @Router /api/conversations/{id}/messages/{msgId}.json [delete]
func DeleteMessage(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	messageID, err := uuid.Parse(c.Params("msgId"))
	if err != nil {
		return c.JSON(errorResponse("Invalid message id"))
	}

	convID, err := repo.DeleteMessage(messageID, user.ID)
	if errors.Is(err, repo.ErrMessageNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse("Message not found"))
	}
	if errors.Is(err, repo.ErrMessageForbidden) {
		return c.Status(fiber.StatusForbidden).JSON(errorResponse("You can only delete your own messages"))
	}
	if err != nil {
		return c.JSON(errorResponse("Failed to delete message"))
	}

	go func() {
		userIDs, err := repo.GetConversationMemberIDs(convID)
		if err != nil {
			return
		}
		chatHub.sendToUsers(userIDs, chatWSEvent{
			Type:           "message_deleted",
			ConversationID: convID,
			Data: map[string]interface{}{
				"message_id": messageID,
			},
		})
	}()

	return c.JSON(successResponse("Message deleted", nil))
}

// DeleteConversation removes current user from a conversation.
// @Summary Delete conversation for current user
// @Tags Chat
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Conversation ID"
// @Success 200 {object} conversationResponse
// @Router /api/conversations/{id}.json [delete]
func DeleteConversation(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	conversationID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid conversation id"))
	}

	if err := repo.DeleteConversation(conversationID, user.ID); err != nil {
		if errors.Is(err, repo.ErrConversationForbidden) {
			return c.Status(fiber.StatusForbidden).JSON(errorResponse("Access denied"))
		}
		return c.JSON(errorResponse("Failed to delete conversation"))
	}

	return c.JSON(successResponse("Conversation deleted", nil))
}

// PurgeConversation permanently deletes a conversation, its messages and its
// memberships for every participant. This cannot be undone.
// @Summary Permanently delete a conversation
// @Tags Chat
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Conversation ID"
// @Success 200 {object} conversationResponse
// @Router /api/conversations/{id}/purge.json [delete]
func PurgeConversation(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	conversationID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid conversation id"))
	}

	// Member IDs must be fetched before purging — nothing to query afterwards.
	memberIDs, _ := repo.GetConversationMemberIDs(conversationID)

	if err := repo.PurgeConversation(conversationID, user.ID); err != nil {
		if errors.Is(err, repo.ErrConversationForbidden) {
			return c.Status(fiber.StatusForbidden).JSON(errorResponse("Access denied"))
		}
		return c.JSON(errorResponse("Failed to delete conversation"))
	}

	go func() {
		chatHub.sendToUsers(memberIDs, chatWSEvent{
			Type:           "conversation_deleted",
			ConversationID: conversationID,
			Data: map[string]interface{}{
				"conversation_id": conversationID,
			},
		})
	}()

	return c.JSON(successResponse("Conversation deleted", nil))
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
	user, err := currentUserFromContext(c)
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
