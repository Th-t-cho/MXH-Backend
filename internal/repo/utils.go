package repo

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type Query struct {
	ID         uuid.UUID  `query:"id"`
	Limit      int        `query:"limit"`
	Page       int        `query:"page"`
	CenterID   uuid.UUID  `query:"center_id"`
	Search     string     `query:"search"`
	Status     string     `query:"status"`
	CategoryID uuid.UUID  `query:"category_id"`
	StartTime  *time.Time `query:"start_time"`
	EndTime    *time.Time `query:"end_time"`
}

func (query *Query) Parse(c *fiber.Ctx) {
	if err := c.QueryParser(query); err != nil {
		query.Limit = 10
		query.Page = 1
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 10
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
}
