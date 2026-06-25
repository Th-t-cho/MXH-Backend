package handler

import (
	"core/internal/model"
	"core/internal/repo"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ─── Response shapes ───────────────────────────────────────────────────────

type reactionStatsResponse struct {
	Like  int `json:"like"`
	Love  int `json:"love"`
	Haha  int `json:"haha"`
	Wow   int `json:"wow"`
	Sad   int `json:"sad"`
	Angry int `json:"angry"`
}

type commentResponse struct {
	ID              string            `json:"id"`
	PostID          string            `json:"post_id"`
	AuthorID        string            `json:"author_id"`
	AuthorName      string            `json:"author_name"`
	AuthorAvatar    string            `json:"author_avatar"`
	Content         string            `json:"content"`
	LikesCount      int               `json:"likes_count"`
	MyReaction      string            `json:"my_reaction,omitempty"`
	ParentCommentID string            `json:"parent_comment_id,omitempty"`
	Replies         []commentResponse `json:"replies"`
	CreatedAt       time.Time         `json:"created_at"`
}

type postResponse struct {
	ID             string                `json:"id"`
	UserID         string                `json:"user_id"`
	AuthorName     string                `json:"author_name"`
	AuthorUsername string                `json:"author_username"`
	AuthorAvatar   string                `json:"author_avatar"`
	Content        string                `json:"content"`
	ImageURL       string                `json:"image_url"`
	Tags           []string              `json:"tags"`
	LikesCount     int                   `json:"likes_count"`
	CommentsCount  int64                 `json:"comments_count"`
	SharesCount    int                   `json:"shares_count"`
	Reactions      reactionStatsResponse `json:"reactions"`
	IsLiked        bool                  `json:"is_liked"`
	MyReaction     string                `json:"my_reaction,omitempty"`
	Comments       []commentResponse     `json:"comments"`
	CreatedAt      time.Time             `json:"created_at"`
}

// buildCommentResponse converts a model.PostComment + replies into the response shape.
func buildCommentResponse(c model.PostComment, myReaction string, replies []commentResponse) commentResponse {
	parentID := ""
	if c.ParentCommentID != nil {
		parentID = c.ParentCommentID.String()
	}
	name := c.User.Name
	if name == "" {
		name = c.User.Username
	}
	return commentResponse{
		ID:              c.ID.String(),
		PostID:          c.PostID.String(),
		AuthorID:        c.UserID.String(),
		AuthorName:      name,
		AuthorAvatar:    c.User.Avatar,
		Content:         c.Content,
		LikesCount:      c.LikesCount,
		MyReaction:      myReaction,
		ParentCommentID: parentID,
		Replies:         replies,
		CreatedAt:       *c.CreatedAt,
	}
}

// buildPostResponse assembles a postResponse from a model.Post plus preloaded data.
func buildPostResponse(
	p model.Post,
	tags []string,
	stats repo.ReactionStats,
	myReaction string,
	commentsCount int64,
	comments []commentResponse,
) postResponse {
	authorName := p.User.Name
	if authorName == "" {
		authorName = p.User.Username
	}
	if tags == nil {
		tags = []string{}
	}
	return postResponse{
		ID:             p.ID.String(),
		UserID:         p.UserID.String(),
		AuthorName:     authorName,
		AuthorUsername: p.User.Username,
		AuthorAvatar:   p.User.Avatar,
		Content:        p.Content,
		ImageURL:       p.ImageURL,
		Tags:           tags,
		LikesCount:     stats.Total,
		CommentsCount:  commentsCount,
		SharesCount:    p.SharesCount,
		Reactions: reactionStatsResponse{
			Like:  stats.Like,
			Love:  stats.Love,
			Haha:  stats.Haha,
			Wow:   stats.Wow,
			Sad:   stats.Sad,
			Angry: stats.Angry,
		},
		IsLiked:    myReaction != "",
		MyReaction: myReaction,
		Comments:   comments,
		CreatedAt:  *p.CreatedAt,
	}
}

// loadCommentsForPost fetches top-level comments + replies and returns the response slice.
func loadCommentsForPost(postID uuid.UUID, callerID uuid.UUID) ([]commentResponse, error) {
	topComments, err := repo.GetCommentsByPost(postID)
	if err != nil {
		return nil, err
	}
	if len(topComments) == 0 {
		return []commentResponse{}, nil
	}

	// Collect IDs for batch reaction fetch
	topIDs := make([]uuid.UUID, len(topComments))
	for i, c := range topComments {
		topIDs[i] = c.ID
	}

	replies, err := repo.GetRepliesByCommentIDs(topIDs)
	if err != nil {
		return nil, err
	}

	// Build IDs for all (top + reply) to batch-fetch reactions
	allIDs := make([]uuid.UUID, 0, len(topComments)+len(replies))
	allIDs = append(allIDs, topIDs...)
	for _, r := range replies {
		allIDs = append(allIDs, r.ID)
	}

	myReactions, err := repo.GetUserCommentReactionsBatch(allIDs, callerID)
	if err != nil {
		myReactions = map[uuid.UUID]string{}
	}

	// Map replies to their parent
	replyMap := make(map[uuid.UUID][]commentResponse)
	for _, r := range replies {
		if r.ParentCommentID == nil {
			continue
		}
		pid := *r.ParentCommentID
		replyMap[pid] = append(replyMap[pid], buildCommentResponse(r, myReactions[r.ID], []commentResponse{}))
	}

	result := make([]commentResponse, 0, len(topComments))
	for _, c := range topComments {
		reps := replyMap[c.ID]
		if reps == nil {
			reps = []commentResponse{}
		}
		result = append(result, buildCommentResponse(c, myReactions[c.ID], reps))
	}
	return result, nil
}

// ─── Create Post ────────────────────────────────────────────────────────────

type createPostRequest struct {
	Content  string   `json:"content"`
	ImageURL string   `json:"image_url"`
	Tags     []string `json:"tags"`
}

// CreatePost creates a new post.
// @Summary Create post
// @Tags Posts
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param request body createPostRequest true "Post data"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts.json [post]
func CreatePost(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	req := createPostRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		return c.JSON(errorResponse("Content is required"))
	}

	post := model.Post{
		UserID:   user.ID,
		Content:  content,
		ImageURL: strings.TrimSpace(req.ImageURL),
	}

	if err := repo.CreatePost(&post, req.Tags); err != nil {
		return c.JSON(errorResponse("Failed to create post"))
	}

	// Reload with User preloaded
	created, err := repo.GetPostByID(post.ID)
	if err != nil {
		return c.JSON(errorResponse("Failed to load post"))
	}

	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	resp := buildPostResponse(created, tags, repo.ReactionStats{}, "", 0, []commentResponse{})
	return c.JSON(successResponse("Post created", fiber.Map{"data": resp}))
}

// ─── List Posts ─────────────────────────────────────────────────────────────

// ListPosts returns a paginated feed.
// @Summary List posts
// @Tags Posts
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param page query int false "Page number (default 1)"
// @Param limit query int false "Items per page (default 20, max 50)"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts.json [get]
func ListPosts(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)

	posts, err := repo.ListPosts(page, limit)
	if err != nil {
		return c.JSON(errorResponse("Failed to load posts"))
	}
	if len(posts) == 0 {
		return c.JSON(successResponse("Posts found", fiber.Map{"data": []interface{}{}}))
	}

	// Batch-load tags, reactions, comment counts
	postIDs := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		postIDs[i] = p.ID
	}

	tagsMap, _ := repo.GetPostTagsMap(postIDs)
	statsMap, _ := repo.GetPostReactionStatsBatch(postIDs)
	myReactionsMap, _ := repo.GetUserPostReactionsBatch(postIDs, user.ID)
	commentCountsMap, _ := repo.CountPostCommentsBatch(postIDs)

	data := make([]postResponse, 0, len(posts))
	for _, p := range posts {
		tags := tagsMap[p.ID]
		stats := statsMap[p.ID]
		myRx := myReactionsMap[p.ID]
		count := commentCountsMap[p.ID]

		// Load comments for each post (latest 20)
		comments, _ := loadCommentsForPost(p.ID, user.ID)

		data = append(data, buildPostResponse(p, tags, stats, myRx, count, comments))
	}

	return c.JSON(successResponse("Posts found", fiber.Map{"data": data, "page": page, "limit": limit}))
}

// ─── Get Post ───────────────────────────────────────────────────────────────

// GetPost returns a single post with full details.
// @Summary Get post
// @Tags Posts
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}.json [get]
func GetPost(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid post ID"))
	}

	post, err := repo.GetPostByID(postID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(errorResponse("Post not found"))
	}
	if err != nil {
		return c.JSON(errorResponse("Failed to load post"))
	}

	tags, _ := repo.GetPostTagsMap([]uuid.UUID{postID})
	statsMap, _ := repo.GetPostReactionStatsBatch([]uuid.UUID{postID})
	myRxMap, _ := repo.GetUserPostReactionsBatch([]uuid.UUID{postID}, user.ID)
	count, _ := repo.CountPostComments(postID)
	comments, _ := loadCommentsForPost(postID, user.ID)

	resp := buildPostResponse(post, tags[postID], statsMap[postID], myRxMap[postID], count, comments)
	return c.JSON(successResponse("Post found", fiber.Map{"data": resp}))
}

// ─── Delete Post ─────────────────────────────────────────────────────────────

// DeletePost removes a post (owner only).
// @Summary Delete post
// @Tags Posts
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}.json [delete]
func DeletePost(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid post ID"))
	}

	if err := repo.DeletePost(postID, user.ID); err != nil {
		return c.JSON(errorResponse(err.Error()))
	}

	return c.JSON(successResponse("Post deleted"))
}

// ─── React to Post ──────────────────────────────────────────────────────────

type reactRequest struct {
	ReactionType string `json:"reaction_type"`
}

// ReactPost adds, changes, or removes a reaction on a post.
// @Summary React to post
// @Tags Posts
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Param request body reactRequest true "Reaction"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}/react.json [post]
func ReactPost(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid post ID"))
	}

	req := reactRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	validTypes := map[string]bool{"like": true, "love": true, "haha": true, "wow": true, "sad": true, "angry": true}
	if !validTypes[req.ReactionType] {
		req.ReactionType = "like"
	}

	rxType, isReacted, err := repo.TogglePostReaction(postID, user.ID, req.ReactionType)
	if err != nil {
		return c.JSON(errorResponse("Failed to react"))
	}

	return c.JSON(successResponse("Reacted", fiber.Map{
		"reaction_type": rxType,
		"is_reacted":    isReacted,
	}))
}

// ─── Share Post ──────────────────────────────────────────────────────────────

// SharePost increments the share counter for a post.
// @Summary Share post
// @Tags Posts
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}/share.json [post]
func SharePost(c *fiber.Ctx) error {
	_, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid post ID"))
	}

	if err := repo.IncrementPostShares(postID); err != nil {
		return c.JSON(errorResponse("Failed to share post"))
	}

	return c.JSON(successResponse("Post shared"))
}

// ─── Add Comment ─────────────────────────────────────────────────────────────

type addCommentRequest struct {
	Content         string `json:"content"`
	ParentCommentID string `json:"parent_comment_id"`
}

// AddComment adds a comment (or reply) to a post.
// @Summary Add comment
// @Tags Comments
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Param request body addCommentRequest true "Comment data"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}/comments.json [post]
func AddComment(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid post ID"))
	}

	req := addCommentRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		return c.JSON(errorResponse("Content is required"))
	}

	comment := model.PostComment{
		PostID:  postID,
		UserID:  user.ID,
		Content: content,
	}

	if strings.TrimSpace(req.ParentCommentID) != "" {
		parentID, err := uuid.Parse(req.ParentCommentID)
		if err != nil {
			return c.JSON(errorResponse("Invalid parent comment ID"))
		}
		comment.ParentCommentID = &parentID
	}

	if err := repo.CreateComment(&comment); err != nil {
		return c.JSON(errorResponse("Failed to add comment"))
	}

	// Reload with User
	created, err := repo.GetCommentByID(comment.ID)
	if err != nil {
		return c.JSON(errorResponse("Failed to load comment"))
	}

	resp := buildCommentResponse(created, "", []commentResponse{})
	return c.JSON(successResponse("Comment added", fiber.Map{"data": resp}))
}

// ─── List Comments ────────────────────────────────────────────────────────────

// ListComments returns all comments (with replies) for a post.
// @Summary List comments
// @Tags Comments
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}/comments.json [get]
func ListComments(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	postID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.JSON(errorResponse("Invalid post ID"))
	}

	comments, err := loadCommentsForPost(postID, user.ID)
	if err != nil {
		return c.JSON(errorResponse("Failed to load comments"))
	}

	return c.JSON(successResponse("Comments found", fiber.Map{"data": comments}))
}

// ─── Delete Comment ───────────────────────────────────────────────────────────

// DeleteComment removes a comment (owner only).
// @Summary Delete comment
// @Tags Comments
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Param cid path string true "Comment ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}/comments/{cid}.json [delete]
func DeleteComment(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	commentID, err := uuid.Parse(c.Params("cid"))
	if err != nil {
		return c.JSON(errorResponse("Invalid comment ID"))
	}

	if err := repo.DeleteComment(commentID, user.ID); err != nil {
		return c.JSON(errorResponse(err.Error()))
	}

	return c.JSON(successResponse("Comment deleted"))
}

// ─── React to Comment ─────────────────────────────────────────────────────────

// ReactComment adds, changes, or removes a reaction on a comment.
// @Summary React to comment
// @Tags Comments
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param id path string true "Post ID"
// @Param cid path string true "Comment ID"
// @Param request body reactRequest true "Reaction"
// @Success 200 {object} map[string]interface{}
// @Router /api/posts/{id}/comments/{cid}/react.json [post]
func ReactComment(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	commentID, err := uuid.Parse(c.Params("cid"))
	if err != nil {
		return c.JSON(errorResponse("Invalid comment ID"))
	}

	req := reactRequest{}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(errorResponse("Invalid request body"))
	}

	validTypes := map[string]bool{"like": true, "love": true, "haha": true, "wow": true, "sad": true, "angry": true}
	if !validTypes[req.ReactionType] {
		req.ReactionType = "like"
	}

	rxType, isReacted, err := repo.ToggleCommentReaction(commentID, user.ID, req.ReactionType)
	if err != nil {
		return c.JSON(errorResponse("Failed to react"))
	}

	return c.JSON(successResponse("Reacted", fiber.Map{
		"reaction_type": rxType,
		"is_reacted":    isReacted,
	}))
}
