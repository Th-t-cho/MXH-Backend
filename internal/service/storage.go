package service

import (
	"context"
	"core/app"
	"errors"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type UploadedMedia struct {
	Key      string `json:"key"`
	URL      string `json:"url,omitempty"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

func UploadMediaToStorage(ctx context.Context, key string, body io.Reader, size int64, mimeType string) (UploadedMedia, error) {
	storage, err := r2StorageConfig()
	if err != nil {
		return UploadedMedia{}, err
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("auto"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(storage.accessKeyID, storage.secretAccessKey, "")),
	)
	if err != nil {
		return UploadedMedia{}, err
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(storage.endpoint())
		options.UsePathStyle = true
	})

	key = strings.TrimLeft(path.Clean(key), "/")
	if key == "." || key == "" {
		return UploadedMedia{}, errors.New("invalid storage key")
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(storage.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(mimeType),
	})
	if err != nil {
		return UploadedMedia{}, err
	}

	return UploadedMedia{
		Key:      key,
		URL:      storage.publicURL(key),
		MimeType: mimeType,
		Size:     size,
	}, nil
}

type storageConfig struct {
	accessKeyID     string
	secretAccessKey string
	bucket          string
	accountID       string
	publicBaseURL   string
}

func r2StorageConfig() (storageConfig, error) {
	cfg := storageConfig{
		accountID:       strings.TrimSpace(app.Config("R2_ACCOUNT_ID")),
		accessKeyID:     strings.TrimSpace(app.Config("R2_ACCESS_KEY_ID")),
		secretAccessKey: strings.TrimSpace(app.Config("R2_SECRET_ACCESS_KEY")),
		bucket:          strings.TrimSpace(app.Config("R2_BUCKET")),
		publicBaseURL:   strings.TrimSpace(app.Config("R2_PUBLIC_BASE_URL")),
	}
	if !cfg.isComplete() {
		return storageConfig{}, errors.New("R2 config is incomplete")
	}
	return cfg, nil
}

func (cfg storageConfig) isComplete() bool {
	return cfg.accountID != "" && cfg.accessKeyID != "" && cfg.secretAccessKey != "" && cfg.bucket != ""
}

func (cfg storageConfig) endpoint() string {
	return "https://" + cfg.accountID + ".r2.cloudflarestorage.com"
}

func (cfg storageConfig) publicURL(key string) string {
	baseURL := strings.TrimRight(cfg.publicBaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(cfg.endpoint(), "/") + "/" + strings.TrimLeft(cfg.bucket, "/")
	}
	return baseURL + "/" + strings.TrimLeft(key, "/")
}

func ExtensionFromMimeType(mimeType string) string {
	extensions, err := mime.ExtensionsByType(mimeType)
	if err != nil || len(extensions) == 0 {
		return ""
	}
	return extensions[0]
}
