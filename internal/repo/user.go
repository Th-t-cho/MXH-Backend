package repo

import (
	"core/app"
	"core/internal/model"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrOTPInvalid = errors.New("otp invalid")
var ErrOTPExpired = errors.New("otp expired")
var ErrOTPTooManyAttempts = errors.New("otp too many attempts")

func GetUser(username string) (model.User, error) {
	user := model.User{}
	err := app.Database.DB.Where("username = ?", username).First(&user).Error
	return user, err
}

func GetUserByID(id uuid.UUID) (model.User, error) {
	user := model.User{}
	tx := app.Database.DB.Where("id = ?", id).First(&user).Error
	return user, tx
}

func GetUserByEmail(email string) (model.User, error) {
	user := model.User{}
	err := app.Database.DB.Where("email = ?", NormalizeEmail(email)).First(&user).Error
	return user, err
}

func GetUserByAccount(account string) (model.User, error) {
	account = strings.TrimSpace(account)
	user := model.User{}
	err := app.Database.DB.
		Where("username = ? OR email = ?", account, NormalizeEmail(account)).
		First(&user).Error
	return user, err
}

func CreateUser(user *model.User) error {
	user.Email = NormalizeEmail(user.Email)
	user.Username = strings.TrimSpace(user.Username)
	return app.Database.DB.Create(user).Error
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func CreateRegisterOTP(email string, codeHash string, expiresAt time.Time) error {
	return createUserOTP(email, codeHash, "register", expiresAt)
}

func CreatePasswordResetOTP(email string, codeHash string, expiresAt time.Time) error {
	return createUserOTP(email, codeHash, "forgot_password", expiresAt)
}

func createUserOTP(email string, codeHash string, purpose string, expiresAt time.Time) error {
	otp := model.UserOTP{
		Email:     NormalizeEmail(email),
		CodeHash:  codeHash,
		Purpose:   purpose,
		ExpiresAt: expiresAt,
	}
	return app.Database.DB.Create(&otp).Error
}

func VerifyRegisterOTP(email string, codeHash string, consume bool) error {
	return verifyUserOTP(email, codeHash, "register", consume)
}

func VerifyPasswordResetOTP(email string, codeHash string, consume bool) error {
	return verifyUserOTP(email, codeHash, "forgot_password", consume)
}

func verifyUserOTP(email string, codeHash string, purpose string, consume bool) error {
	now := time.Now()
	otp := model.UserOTP{}
	err := app.Database.DB.
		Where("email = ? AND purpose = ? AND consumed_at IS NULL", NormalizeEmail(email), purpose).
		Order("created_at DESC").
		First(&otp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrOTPInvalid
	}
	if err != nil {
		return err
	}
	if otp.ExpiresAt.Before(now) {
		return ErrOTPExpired
	}
	if otp.Attempts >= 5 {
		return ErrOTPTooManyAttempts
	}
	if otp.CodeHash != codeHash {
		app.Database.DB.Model(&otp).Update("attempts", otp.Attempts+1)
		return ErrOTPInvalid
	}
	if consume {
		return app.Database.DB.Model(&otp).Updates(map[string]interface{}{
			"consumed_at": now,
			"attempts":    otp.Attempts + 1,
		}).Error
	}
	return app.Database.DB.Model(&otp).Update("attempts", otp.Attempts+1).Error
}

func UpdateUserPassword(userID uuid.UUID, password string) error {
	return app.Database.DB.Model(&model.User{}).
		Where("id = ?", userID).
		Update("password", password).Error
}

func UpdateUserLastSeen(userID uuid.UUID) error {
	now := time.Now()
	return app.Database.DB.Model(&model.User{}).
		Where("id = ?", userID).
		Update("last_seen_at", now).Error
}

func SearchUsers(query string, excludeID uuid.UUID, limit int) ([]model.User, error) {
	if limit <= 0 {
		limit = 20
	}
	q := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	var users []model.User
	err := app.Database.DB.
		Where("id != ? AND status = 'active' AND (LOWER(name) LIKE ? OR LOWER(username) LIKE ?)", excludeID, q, q).
		Limit(limit).
		Find(&users).Error
	return users, err
}
