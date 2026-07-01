package repo

import (
	"core/app"
	"core/internal/model"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrConversationForbidden = errors.New("conversation forbidden")
var ErrInvalidConversationMember = errors.New("invalid conversation member")
var ErrMessageForbidden = errors.New("message forbidden")
var ErrMessageNotFound = errors.New("message not found")

func GetOrCreateDirectConversation(userID uuid.UUID, otherUserID uuid.UUID) (model.Conversation, error) {
	if userID == uuid.Nil || otherUserID == uuid.Nil || userID == otherUserID {
		return model.Conversation{}, ErrInvalidConversationMember
	}

	if _, err := GetUserByID(otherUserID); err != nil {
		return model.Conversation{}, err
	}

	if conversation, err := findDirectConversation(userID, otherUserID); err == nil {
		if err := app.Database.DB.Unscoped().
			Model(&model.ConversationMember{}).
			Where("conversation_id = ? AND user_id = ?", conversation.ID, userID).
			Update("deleted_at", nil).Error; err != nil {
			return model.Conversation{}, err
		}
		return GetConversationForUser(conversation.ID, userID)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Conversation{}, err
	}

	conversation := model.Conversation{
		Type: "direct",
		Members: []model.ConversationMember{
			{UserID: userID},
			{UserID: otherUserID},
		},
	}

	if err := app.Database.DB.Create(&conversation).Error; err != nil {
		return model.Conversation{}, err
	}

	return GetConversationForUser(conversation.ID, userID)
}

func findDirectConversation(userID uuid.UUID, otherUserID uuid.UUID) (model.Conversation, error) {
	conversation := model.Conversation{}
	err := app.Database.DB.
		Joins("JOIN conversation_members cm1 ON cm1.conversation_id = conversations.id AND cm1.user_id = ?", userID).
		Joins("JOIN conversation_members cm2 ON cm2.conversation_id = conversations.id AND cm2.user_id = ?", otherUserID).
		Where("conversations.type = ?", "direct").
		Preload("Members.User").
		First(&conversation).Error
	return conversation, err
}

func ListConversations(userID uuid.UUID) ([]model.Conversation, error) {
	conversations := []model.Conversation{}
	err := app.Database.DB.
		Joins("JOIN conversation_members cm ON cm.conversation_id = conversations.id AND cm.user_id = ? AND cm.deleted_at IS NULL", userID).
		Preload("Members.User").
		Where("conversations.last_message_at IS NOT NULL").
		Order("conversations.last_message_at DESC").
		Find(&conversations).Error
	if err != nil {
		return nil, err
	}

	for i := range conversations {
		message := model.Message{}
		err := app.Database.DB.
			Preload("Sender").
			Where("conversation_id = ?", conversations[i].ID).
			Order("created_at DESC").
			First(&message).Error
		if err == nil {
			conversations[i].LastMessage = &message
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	return conversations, nil
}

func GetConversationForUser(conversationID uuid.UUID, userID uuid.UUID) (model.Conversation, error) {
	if ok, err := IsConversationMember(conversationID, userID); err != nil {
		return model.Conversation{}, err
	} else if !ok {
		return model.Conversation{}, ErrConversationForbidden
	}

	conversation := model.Conversation{}
	err := app.Database.DB.
		Preload("Members.User").
		First(&conversation, "id = ?", conversationID).Error
	return conversation, err
}

func IsConversationMember(conversationID uuid.UUID, userID uuid.UUID) (bool, error) {
	var count int64
	err := app.Database.DB.Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Count(&count).Error
	return count > 0, err
}

func GetConversationMemberIDs(conversationID uuid.UUID) ([]uuid.UUID, error) {
	members := []model.ConversationMember{}
	if err := app.Database.DB.Where("conversation_id = ?", conversationID).Find(&members).Error; err != nil {
		return nil, err
	}

	userIDs := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}
	return userIDs, nil
}

func CreateMessage(conversationID uuid.UUID, senderID uuid.UUID, content string) (model.Message, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return model.Message{}, gorm.ErrInvalidData
	}

	if ok, err := IsConversationMember(conversationID, senderID); err != nil {
		return model.Message{}, err
	} else if !ok {
		return model.Message{}, ErrConversationForbidden
	}

	message := model.Message{
		ConversationID: conversationID,
		SenderID:       senderID,
		Content:        content,
		MessageType:    "text",
	}

	err := app.Database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		return tx.Model(&model.Conversation{}).
			Where("id = ?", conversationID).
			Update("last_message_at", time.Now()).Error
	})
	if err != nil {
		return model.Message{}, err
	}

	return GetMessageByID(message.ID)
}

func GetMessageByID(messageID uuid.UUID) (model.Message, error) {
	message := model.Message{}
	err := app.Database.DB.
		Preload("Sender").
		First(&message, "id = ?", messageID).Error
	return message, err
}

func ListMessages(conversationID uuid.UUID, userID uuid.UUID, query Query) ([]model.Message, error) {
	if ok, err := IsConversationMember(conversationID, userID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrConversationForbidden
	}

	messages := []model.Message{}
	offset := (query.Page - 1) * query.Limit
	err := app.Database.DB.
		Unscoped().
		Preload("Sender").
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC").
		Limit(query.Limit).
		Offset(offset).
		Find(&messages).Error
	if err != nil {
		return nil, err
	}

	// Deleted messages are kept (soft delete) so the thread still shows a
	// placeholder for them, but their real content must not be sent to clients.
	for i := range messages {
		if messages[i].DeletedAt != nil && messages[i].DeletedAt.Valid {
			messages[i].Content = ""
		}
	}

	return messages, nil
}

func MarkConversationRead(conversationID uuid.UUID, userID uuid.UUID) error {
	return app.Database.DB.Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Update("last_read_at", clause.Expr{SQL: "NOW()"}).Error
}

func UpdateMessage(messageID uuid.UUID, senderID uuid.UUID, content string) (model.Message, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return model.Message{}, gorm.ErrInvalidData
	}

	message := model.Message{}
	if err := app.Database.DB.First(&message, "id = ?", messageID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Message{}, ErrMessageNotFound
		}
		return model.Message{}, err
	}

	if message.SenderID != senderID {
		return model.Message{}, ErrMessageForbidden
	}

	if err := app.Database.DB.Model(&message).Update("content", content).Error; err != nil {
		return model.Message{}, err
	}

	return GetMessageByID(messageID)
}

func DeleteMessage(messageID uuid.UUID, senderID uuid.UUID) (uuid.UUID, error) {
	message := model.Message{}
	if err := app.Database.DB.Unscoped().First(&message, "id = ?", messageID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, ErrMessageNotFound
		}
		return uuid.Nil, err
	}

	if message.SenderID != senderID {
		return uuid.Nil, ErrMessageForbidden
	}

	convID := message.ConversationID
	if message.DeletedAt != nil && message.DeletedAt.Valid {
		// Already deleted — treat as a no-op so a duplicate/late request
		// (double-tap, retry) doesn't surface as a 404 error.
		return convID, nil
	}

	return convID, app.Database.DB.Delete(&message).Error
}

func DeleteConversation(conversationID uuid.UUID, userID uuid.UUID) error {
	ok, err := IsConversationMember(conversationID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrConversationForbidden
	}

	return app.Database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("conversation_id = ? AND user_id = ?", conversationID, userID).
			Delete(&model.ConversationMember{}).Error; err != nil {
			return err
		}

		var remaining int64
		if err := tx.Model(&model.ConversationMember{}).
			Where("conversation_id = ?", conversationID).
			Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 {
			if err := tx.Delete(&model.Conversation{}, "id = ?", conversationID).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func PurgeConversation(conversationID uuid.UUID, userID uuid.UUID) error {
	ok, err := IsConversationMember(conversationID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrConversationForbidden
	}

	return app.Database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().
			Where("conversation_id = ?", conversationID).
			Delete(&model.Message{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().
			Where("conversation_id = ?", conversationID).
			Delete(&model.ConversationMember{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&model.Conversation{}, "id = ?", conversationID).Error
	})
}
