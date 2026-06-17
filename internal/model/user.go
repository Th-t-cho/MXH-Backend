package model

import "time"

type User struct {
	Model    `gorm:"embedded"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty" gorm:"uniqueIndex"`
	Username string `json:"username,omitempty" gorm:"uniqueIndex"`
	Phone    string `json:"phone,omitempty"`
	Password string `json:"-"`
	Role     string `json:"role,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   string `json:"status,omitempty" gorm:"default:'active'"`
	IsVerify bool   `json:"is_verify,omitempty" gorm:"default:false"`
}

type UserOTP struct {
	Model      `gorm:"embedded"`
	Email      string     `json:"email,omitempty" gorm:"index"`
	CodeHash   string     `json:"-" gorm:"size:128;not null"`
	Purpose    string     `json:"purpose,omitempty" gorm:"index;size:64;default:'register'"`
	Attempts   int        `json:"attempts,omitempty" gorm:"default:0"`
	ExpiresAt  time.Time  `json:"expires_at,omitempty" gorm:"index"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
}
