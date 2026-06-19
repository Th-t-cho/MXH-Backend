package app

import (
	"core/internal/model"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const DBTimeout = 10 * time.Second

type DatabaseConfig struct {
	*gorm.DB
	Driver      string `env:"DB_DRIVER"`
	Host        string `env:"DB_HOST"`
	Username    string `env:"DB_USER"`
	Password    string `env:"DB_PASSWORD"`
	DBName      string `env:"DB_NAME"`
	Port        int    `env:"DB_PORT"`
	MaxIdleConn int    `env:"MAX_IDLE_CONN"`
	MaxOpenConn int    `env:"MAX_OPEN_CONN"`
	MaxLifetime int    `env:"MAX_LIFE_TIME_PER_CONN"`
	Debug       bool
}

// Setup initializes the database connection and auto-migrate models
func (dbConf *DatabaseConfig) Setup() {

	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Silent,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	loc := time.FixedZone("UTC+7", 7*60*60)
	time.Local = loc

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&time_zone=%%27%%2B07:00%%27", dbConf.Username, dbConf.Password, dbConf.Host, dbConf.Port, dbConf.DBName)

	db, err := gorm.Open(
		mysql.New(mysql.Config{
			DSN:               dsn,
			DefaultStringSize: 256,
		}),
		&gorm.Config{
			PrepareStmt: true,
			Logger:      newLogger,
			NowFunc: func() time.Time {
				return time.Now().In(loc)
			},
		},
	)

	if err != nil {
		logrus.Fatal("Failed to connect to database:", err)
	}

	if dbConf.Debug {
		db = db.Debug()
	}

	sqlDB, err := db.DB()
	if err != nil {
		logrus.Fatal("Failed to get sql.DB from gorm:", err)
	}

	// Set connection pool from env.
	if dbConf.MaxOpenConn > 0 {
		sqlDB.SetMaxOpenConns(dbConf.MaxOpenConn)
	} else {
		sqlDB.SetMaxOpenConns(20) // default
	}

	if dbConf.MaxIdleConn > 0 {
		sqlDB.SetMaxIdleConns(dbConf.MaxIdleConn)
	} else {
		sqlDB.SetMaxIdleConns(10) // default
	}

	if dbConf.MaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(dbConf.MaxLifetime) * time.Minute)
	} else {
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	dbConf.DB = db

	models := []interface{}{
		&model.User{},
		&model.UserOTP{},
		&model.Conversation{},
		&model.ConversationMember{},
		&model.Message{},
	}

	for _, m := range models {
		if err := db.AutoMigrate(m); err != nil {
			logrus.Warn("AutoMigrate error:", err)
		}
	}

	// Composite indexes for hot query paths — IF NOT EXISTS prevents duplicate errors on restart
	rawIndexes := []string{
		// ListMessages: WHERE conversation_id = ? ORDER BY created_at DESC
		`CREATE INDEX IF NOT EXISTS idx_messages_conv_created ON messages (conversation_id, created_at DESC)`,
	}
	for _, sql := range rawIndexes {
		if err := db.Exec(sql).Error; err != nil {
			logrus.Warn("Index creation warning:", err)
		}
	}

	logrus.Info("Database connection established & migration completed")
}
