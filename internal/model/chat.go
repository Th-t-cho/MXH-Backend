package model

import (
	"time"

	"github.com/google/uuid"
)

type Conversation struct {
	Model         `gorm:"embedded"`
	Type          string               `json:"type,omitempty" gorm:"index;size:32;default:'direct'"`
	LastMessageAt *time.Time           `json:"last_message_at,omitempty" gorm:"index"`
	Members       []ConversationMember `json:"members,omitempty" gorm:"foreignKey:ConversationID"`
}

type ConversationMember struct {
	Model          `gorm:"embedded"`
	ConversationID uuid.UUID  `json:"conversation_id,omitempty" gorm:"index;not null"`
	UserID         uuid.UUID  `json:"user_id,omitempty" gorm:"index;not null"`
	User           User       `json:"user,omitempty" gorm:"foreignKey:UserID"`
	LastReadAt     *time.Time `json:"last_read_at,omitempty"`
}

type Message struct {
	Model          `gorm:"embedded"`
	ConversationID uuid.UUID `json:"conversation_id,omitempty" gorm:"index;not null"`
	SenderID       uuid.UUID `json:"sender_id,omitempty" gorm:"index;not null"`
	Sender         User      `json:"sender,omitempty" gorm:"foreignKey:SenderID"`
	Content        string    `json:"content,omitempty" gorm:"type:text;not null"`
	MessageType    string    `json:"message_type,omitempty" gorm:"size:32;default:'text'"`
}
