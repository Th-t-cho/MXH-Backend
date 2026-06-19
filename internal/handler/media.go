package handler

import (
	"bytes"
	"core/app"
	"core/internal/service"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	xdraw "golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"
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

	body, size, uploadMimeType, err := prepareUploadBody(file, fileHeader.Size, mimeType)
	if err != nil {
		return c.JSON(errorResponse("Failed to process file"))
	}

	key := mediaStorageKey(user.ID.String(), uploadMimeType, fileHeader.Filename)
	media, err := service.UploadMediaToStorage(c.Context(), key, body, size, uploadMimeType)
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

func prepareUploadBody(file io.ReadSeeker, size int64, mimeType string) (io.Reader, int64, string, error) {
	if !strings.HasPrefix(mimeType, "image/") || mimeType == "image/gif" {
		return file, size, mimeType, nil
	}

	resized, err := resizeImage(file)
	if err != nil {
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			return nil, 0, "", seekErr
		}
		return file, size, mimeType, nil
	}

	return bytes.NewReader(resized), int64(len(resized)), "image/jpeg", nil
}

func resizeImage(file io.ReadSeeker) ([]byte, error) {
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	maxDimension := imageMaxDimension()
	if width > maxDimension || height > maxDimension {
		width, height = resizeDimensions(width, height, maxDimension)
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		fillImage(dst, color.White)
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, xdraw.Over, nil)
		img = dst
	} else {
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		fillImage(dst, color.White)
		stddraw.Draw(dst, dst.Bounds(), img, bounds.Min, stddraw.Over)
		img = dst
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: imageJPEGQuality()}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func fillImage(dst stddraw.Image, color color.Color) {
	stddraw.Draw(dst, dst.Bounds(), image.NewUniform(color), image.Point{}, stddraw.Src)
}

func resizeDimensions(width int, height int, maxDimension int) (int, int) {
	if width >= height {
		return maxDimension, height * maxDimension / width
	}
	return width * maxDimension / height, maxDimension
}

func mediaStorageKey(userID string, mimeType string, filename string) string {
	ext := service.ExtensionFromMimeType(mimeType)
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(filename))
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

func imageMaxDimension() int {
	maxDimension, err := strconv.Atoi(app.Config("IMAGE_MAX_DIMENSION"))
	if err != nil || maxDimension <= 0 {
		maxDimension = 1600
	}
	return maxDimension
}

func imageJPEGQuality() int {
	quality, err := strconv.Atoi(app.Config("IMAGE_JPEG_QUALITY"))
	if err != nil || quality <= 0 || quality > 100 {
		quality = 82
	}
	return quality
}
