package handler

import (
	"core/app"
	"core/internal/service"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type uploadMediaResponse struct {
	Status  bool                  `json:"status"`
	Message string                `json:"message"`
	Data    service.UploadedMedia `json:"data"`
}

// UploadMedia uploads an image or video file to S3-compatible storage.
// @Summary Upload media
// @Tags Media
// @Accept multipart/form-data
// @Produce json
// @Param Authorization header string true "Bearer access token"
// @Param file formData file true "Image or video file"
// @Success 200 {object} uploadMediaResponse
// @Router /api/media/upload.json [post]
func UploadMedia(c *fiber.Ctx) error {
	user, err := currentUserFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(errorResponse("Unauthorized"))
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.JSON(errorResponse("File is required"))
	}
	if fileHeader.Size <= 0 {
		return c.JSON(errorResponse("File is empty"))
	}
	if fileHeader.Size > maxUploadBytes() {
		return c.JSON(errorResponse("File is too large"))
	}

	file, err := fileHeader.Open()
	if err != nil {
		return c.JSON(errorResponse("Failed to open file"))
	}
	defer file.Close()

	head := make([]byte, 512)
	n, err := file.Read(head)
	if err != nil && err != io.EOF {
		return c.JSON(errorResponse("Failed to read file"))
	}

	mimeType := detectMediaMimeType(head[:n], fileHeader.Header.Get("Content-Type"))
	if !isAllowedMediaMimeType(mimeType) {
		return c.JSON(errorResponse("Only image or video files are allowed"))
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return c.JSON(errorResponse("Failed to read file"))
	}

	key := mediaStorageKey(user.ID.String(), mimeType, fileHeader.Filename)
	media, err := service.UploadMediaToStorage(c.Context(), key, file, fileHeader.Size, mimeType)
	if err != nil {
		fmt.Println("🚀 ~ file: media.go ~ line 71 ~ funcUploadMedia ~ err : ", err)

		return c.JSON(errorResponse("Failed to upload media"))
	}

	return c.JSON(successResponse("Media uploaded", fiber.Map{"data": media}))
}

func detectMediaMimeType(fileHeader []byte, headerMimeType string) string {
	detectedMimeType := http.DetectContentType(fileHeader)
	if isAllowedMediaMimeType(detectedMimeType) {
		return detectedMimeType
	}

	headerMimeType = strings.ToLower(strings.TrimSpace(strings.Split(headerMimeType, ";")[0]))
	if isAllowedMediaMimeType(headerMimeType) {
		return headerMimeType
	}
	return detectedMimeType
}

func isAllowedMediaMimeType(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "video/")
}

func mediaStorageKey(userID string, mimeType string, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = service.ExtensionFromMimeType(mimeType)
	}
	if ext == "" {
		ext = ".bin"
	}

	now := time.Now()
	return strings.Join([]string{
		"chat",
		userID,
		now.Format("2006/01/02"),
		uuid.New().String() + ext,
	}, "/")
}

func maxUploadBytes() int64 {
	megabytes, err := strconv.Atoi(app.Config("UPLOAD_MAX_MB"))
	if err != nil || megabytes <= 0 {
		megabytes = 50
	}
	return int64(megabytes) * 1024 * 1024
}
