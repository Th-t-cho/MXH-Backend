package model

import "github.com/google/uuid"

type Post struct {
	Model     `gorm:"embedded"`
	UserID    uuid.UUID  `json:"user_id,omitempty" gorm:"index;not null"`
	User      User       `json:"user,omitempty" gorm:"foreignKey:UserID"`
	Content   string     `json:"content,omitempty" gorm:"type:text;not null"`
	ImageURL  string     `json:"image_url,omitempty" gorm:"size:512"`
	ShareOfID *uuid.UUID `json:"share_of_id,omitempty" gorm:"index"`
	SharesCount int      `json:"shares_count,omitempty" gorm:"default:0"`
}

type PostTag struct {
	Model  `gorm:"embedded"`
	PostID uuid.UUID `json:"post_id,omitempty" gorm:"index;not null"`
	Tag    string    `json:"tag,omitempty" gorm:"index;size:64;not null"`
}

// PostReaction stores one reaction per user per post (unique idx_pr_post_user).
type PostReaction struct {
	Model        `gorm:"embedded"`
	PostID       uuid.UUID `json:"post_id,omitempty" gorm:"index:idx_pr_post_user,priority:1;not null"`
	UserID       uuid.UUID `json:"user_id,omitempty" gorm:"index:idx_pr_post_user,priority:2;not null"`
	ReactionType string    `json:"reaction_type,omitempty" gorm:"size:16;not null;default:'like'"`
}

type PostComment struct {
	Model           `gorm:"embedded"`
	PostID          uuid.UUID  `json:"post_id,omitempty" gorm:"index;not null"`
	UserID          uuid.UUID  `json:"user_id,omitempty" gorm:"index;not null"`
	User            User       `json:"user,omitempty" gorm:"foreignKey:UserID"`
	ParentCommentID *uuid.UUID `json:"parent_comment_id,omitempty" gorm:"index"`
	Content         string     `json:"content,omitempty" gorm:"type:text;not null"`
	LikesCount      int        `json:"likes_count,omitempty" gorm:"default:0"`
}

// PostCommentReaction stores one reaction per user per comment (unique idx_pcr_comment_user).
type PostCommentReaction struct {
	Model        `gorm:"embedded"`
	CommentID    uuid.UUID `json:"comment_id,omitempty" gorm:"index:idx_pcr_comment_user,priority:1;not null"`
	UserID       uuid.UUID `json:"user_id,omitempty" gorm:"index:idx_pcr_comment_user,priority:2;not null"`
	ReactionType string    `json:"reaction_type,omitempty" gorm:"size:16;not null;default:'like'"`
}
