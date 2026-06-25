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

// ─── Post ──────────────────────────────────────────────────────────────────

func CreatePost(post *model.Post, tags []string) error {
	return app.Database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(post).Error; err != nil {
			return err
		}
		for _, tag := range tags {
			tag = strings.TrimSpace(strings.ToLower(tag))
			if tag == "" {
				continue
			}
			pt := model.PostTag{PostID: post.ID, Tag: tag}
			if err := tx.Create(&pt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func GetPostByID(postID uuid.UUID) (model.Post, error) {
	var post model.Post
	err := app.Database.DB.Preload("User").
		Where("id = ?", postID).First(&post).Error
	return post, err
}

// ListPosts returns a paginated feed sorted newest-first.
func ListPosts(page, limit int) ([]model.Post, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	offset := (page - 1) * limit
	var posts []model.Post
	err := app.Database.DB.
		Preload("User").
		Order("created_at DESC").
		Offset(offset).Limit(limit).
		Find(&posts).Error
	return posts, err
}

func DeletePost(postID, userID uuid.UUID) error {
	result := app.Database.DB.
		Where("id = ? AND user_id = ?", postID, userID).
		Delete(&model.Post{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("post not found or unauthorized")
	}
	return nil
}

func IncrementPostShares(postID uuid.UUID) error {
	return app.Database.DB.Model(&model.Post{}).
		Where("id = ?", postID).
		UpdateColumn("shares_count", gorm.Expr("shares_count + 1")).Error
}

// ─── Tags ──────────────────────────────────────────────────────────────────

// GetPostTagsMap returns tags for a set of posts, keyed by post ID.
func GetPostTagsMap(postIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID][]string{}, nil
	}
	var tags []model.PostTag
	err := app.Database.DB.Where("post_id IN ?", postIDs).Find(&tags).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID][]string, len(postIDs))
	for _, t := range tags {
		result[t.PostID] = append(result[t.PostID], t.Tag)
	}
	return result, nil
}

// ─── Post Reactions ────────────────────────────────────────────────────────

type ReactionStats struct {
	Like  int `json:"like"`
	Love  int `json:"love"`
	Haha  int `json:"haha"`
	Wow   int `json:"wow"`
	Sad   int `json:"sad"`
	Angry int `json:"angry"`
	Total int `json:"total"`
}

type reactionCount struct {
	ReactionType string
	Count        int
}

func buildStats(counts []reactionCount) ReactionStats {
	s := ReactionStats{}
	for _, c := range counts {
		s.Total += c.Count
		switch c.ReactionType {
		case "like":
			s.Like = c.Count
		case "love":
			s.Love = c.Count
		case "haha":
			s.Haha = c.Count
		case "wow":
			s.Wow = c.Count
		case "sad":
			s.Sad = c.Count
		case "angry":
			s.Angry = c.Count
		}
	}
	return s
}

// GetPostReactionStatsBatch returns reaction stats for multiple posts at once.
func GetPostReactionStatsBatch(postIDs []uuid.UUID) (map[uuid.UUID]ReactionStats, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID]ReactionStats{}, nil
	}
	type batchRow struct {
		PostID       uuid.UUID
		ReactionType string
		Count        int
	}
	var rows []batchRow
	err := app.Database.DB.Model(&model.PostReaction{}).
		Select("post_id, reaction_type, COUNT(*) as count").
		Where("post_id IN ?", postIDs).
		Group("post_id, reaction_type").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Group by post
	grouped := make(map[uuid.UUID][]reactionCount)
	for _, r := range rows {
		grouped[r.PostID] = append(grouped[r.PostID], reactionCount{r.ReactionType, r.Count})
	}
	result := make(map[uuid.UUID]ReactionStats, len(postIDs))
	for pid, counts := range grouped {
		result[pid] = buildStats(counts)
	}
	return result, nil
}

// GetUserPostReactionsBatch returns the calling user's reaction type for each post.
func GetUserPostReactionsBatch(postIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]string, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID]string{}, nil
	}
	var reactions []model.PostReaction
	err := app.Database.DB.
		Where("post_id IN ? AND user_id = ?", postIDs, userID).
		Find(&reactions).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]string, len(reactions))
	for _, r := range reactions {
		result[r.PostID] = r.ReactionType
	}
	return result, nil
}

// TogglePostReaction adds, changes, or removes a reaction (toggle same type off).
// Returns (reactionType, isNowReacted, error).
func TogglePostReaction(postID, userID uuid.UUID, reactionType string) (string, bool, error) {
	var existing model.PostReaction
	err := app.Database.DB.
		Where("post_id = ? AND user_id = ?", postID, userID).
		First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		rx := model.PostReaction{PostID: postID, UserID: userID, ReactionType: reactionType}
		return reactionType, true, app.Database.DB.Create(&rx).Error
	}
	if err != nil {
		return "", false, err
	}

	if existing.ReactionType == reactionType {
		return "", false, app.Database.DB.Delete(&existing).Error
	}

	now := time.Now()
	existing.ReactionType = reactionType
	existing.UpdatedAt = &now
	return reactionType, true, app.Database.DB.
		Model(&existing).Update("reaction_type", reactionType).Error
}

// ─── Comments ──────────────────────────────────────────────────────────────

func CreateComment(comment *model.PostComment) error {
	return app.Database.DB.Create(comment).Error
}

func GetCommentByID(commentID uuid.UUID) (model.PostComment, error) {
	var c model.PostComment
	err := app.Database.DB.Preload("User").
		Where("id = ?", commentID).First(&c).Error
	return c, err
}

// GetCommentsByPost returns top-level comments (no parent) for a post.
func GetCommentsByPost(postID uuid.UUID) ([]model.PostComment, error) {
	var comments []model.PostComment
	err := app.Database.DB.
		Preload("User").
		Where("post_id = ? AND parent_comment_id IS NULL", postID).
		Order("created_at ASC").
		Find(&comments).Error
	return comments, err
}

// GetRepliesByCommentIDs returns all replies for a set of parent comments.
func GetRepliesByCommentIDs(commentIDs []uuid.UUID) ([]model.PostComment, error) {
	if len(commentIDs) == 0 {
		return nil, nil
	}
	var replies []model.PostComment
	err := app.Database.DB.
		Preload("User").
		Where("parent_comment_id IN ?", commentIDs).
		Order("created_at ASC").
		Find(&replies).Error
	return replies, err
}

func DeleteComment(commentID, userID uuid.UUID) error {
	result := app.Database.DB.
		Where("id = ? AND user_id = ?", commentID, userID).
		Delete(&model.PostComment{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("comment not found or unauthorized")
	}
	return nil
}

// CountPostComments returns the total comment+reply count for a post.
func CountPostComments(postID uuid.UUID) (int64, error) {
	var count int64
	err := app.Database.DB.Model(&model.PostComment{}).
		Where("post_id = ?", postID).Count(&count).Error
	return count, err
}

// CountPostCommentsBatch returns comment counts for multiple posts.
func CountPostCommentsBatch(postIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	if len(postIDs) == 0 {
		return map[uuid.UUID]int64{}, nil
	}
	type row struct {
		PostID uuid.UUID
		Count  int64
	}
	var rows []row
	err := app.Database.DB.Model(&model.PostComment{}).
		Select("post_id, COUNT(*) as count").
		Where("post_id IN ?", postIDs).
		Group("post_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		result[r.PostID] = r.Count
	}
	return result, nil
}

// ─── Comment Reactions ─────────────────────────────────────────────────────

// ToggleCommentReaction adds, changes, or removes a comment reaction.
func ToggleCommentReaction(commentID, userID uuid.UUID, reactionType string) (string, bool, error) {
	var existing model.PostCommentReaction
	err := app.Database.DB.
		Where("comment_id = ? AND user_id = ?", commentID, userID).
		First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		rx := model.PostCommentReaction{CommentID: commentID, UserID: userID, ReactionType: reactionType}
		app.Database.DB.Model(&model.PostComment{}).Where("id = ?", commentID).
			UpdateColumn("likes_count", gorm.Expr("likes_count + 1"))
		return reactionType, true, app.Database.DB.Create(&rx).Error
	}
	if err != nil {
		return "", false, err
	}

	if existing.ReactionType == reactionType {
		app.Database.DB.Model(&model.PostComment{}).Where("id = ?", commentID).
			UpdateColumn("likes_count", gorm.Expr("GREATEST(0, likes_count - 1)"))
		return "", false, app.Database.DB.Delete(&existing).Error
	}

	return reactionType, true, app.Database.DB.
		Model(&existing).Update("reaction_type", reactionType).Error
}

// GetUserCommentReactionsBatch returns the user's reactions for a set of comments.
func GetUserCommentReactionsBatch(commentIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]string, error) {
	if len(commentIDs) == 0 {
		return map[uuid.UUID]string{}, nil
	}
	var reactions []model.PostCommentReaction
	err := app.Database.DB.
		Where("comment_id IN ? AND user_id = ?", commentIDs, userID).
		Find(&reactions).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]string, len(reactions))
	for _, r := range reactions {
		result[r.CommentID] = r.ReactionType
	}
	return result, nil
}
