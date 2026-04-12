package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ai-chat/models"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	_ "golang.org/x/image/webp"
)

const maxUploadSizeBytes = 10 * 1024 * 1024

var unsafeFileNameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type OSSService struct {
	bucketName    string
	endpoint      string
	publicBaseURL string
	bucket        *oss.Bucket
}

func NewOSSServiceFromEnv() (*OSSService, error) {
	bucketName := strings.TrimSpace(os.Getenv("OSS_BUCKET"))
	endpoint := strings.TrimSpace(os.Getenv("OSS_ENDPOINT"))
	accessKeyID := strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_ID"))
	accessKeySecret := strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_SECRET"))
	publicBaseURL := strings.TrimSpace(os.Getenv("OSS_PUBLIC_BASE_URL"))

	if bucketName == "" {
		bucketName = "ai-caht"
	}
	if endpoint == "" {
		endpoint = "ai-caht.oss-cn-hongkong.aliyuncs.com"
	}
	if publicBaseURL == "" {
		publicBaseURL = "https://" + bucketName + "." + endpoint
	}

	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("missing OSS credentials")
	}

	client, err := oss.New("https://"+endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("init oss client failed: %w", err)
	}

	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("init oss bucket failed: %w", err)
	}

	return &OSSService{
		bucketName:    bucketName,
		endpoint:      endpoint,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		bucket:        bucket,
	}, nil
}

type UploadedAttachment struct {
	Item models.AttachmentItem
}

func (s *OSSService) UploadImage(
	ctx context.Context,
	userID uint,
	sessionID uint,
	fileHeader *multipart.FileHeader,
) (*UploadedAttachment, error) {
	if fileHeader.Size <= 0 {
		return nil, fmt.Errorf("empty file")
	}
	if fileHeader.Size > maxUploadSizeBytes {
		return nil, fmt.Errorf("file too large")
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("open upload file failed: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read upload file failed: %w", err)
	}
	if int64(len(data)) > maxUploadSizeBytes {
		return nil, fmt.Errorf("file too large")
	}

	mimeType := http.DetectContentType(data)
	if !isAllowedImageMime(mimeType) {
		return nil, fmt.Errorf("unsupported mime type: %s", mimeType)
	}

	width, height, err := decodeImageSize(data)
	if err != nil {
		return nil, fmt.Errorf("decode image size failed: %w", err)
	}

	objectKey := buildObjectKey(userID, sessionID, fileHeader.Filename, mimeType)
	putOptions := []oss.Option{
		oss.ContentType(mimeType),
	}

	if err := s.bucket.PutObject(objectKey, bytes.NewReader(data), putOptions...); err != nil {
		return nil, fmt.Errorf("upload to oss failed: %w", err)
	}

	attachmentID := strings.TrimSuffix(filepath.Base(objectKey), filepath.Ext(objectKey))
	return &UploadedAttachment{
		Item: models.AttachmentItem{
			ID:        attachmentID,
			Type:      "image",
			Name:      fileHeader.Filename,
			MimeType:  mimeType,
			Size:      int64(len(data)),
			RemoteURL: s.publicBaseURL + "/" + objectKey,
			ObjectKey: objectKey,
			Width:     intPtr(width),
			Height:    intPtr(height),
		},
	}, nil
}

func isAllowedImageMime(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func decodeImageSize(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func buildObjectKey(userID uint, sessionID uint, originalName, mimeType string) string {
	datePath := time.Now().Format("20060102")
	ext := filepath.Ext(originalName)
	if ext == "" {
		ext = mimeToExt(mimeType)
	}
	baseName := strings.TrimSuffix(originalName, filepath.Ext(originalName))
	safeName := sanitizeFilename(baseName)
	return fmt.Sprintf(
		"chat-attachments/%d/%d/%s/%d-%s%s",
		userID,
		sessionID,
		datePath,
		time.Now().UnixNano(),
		safeName,
		ext,
	)
}

func sanitizeFilename(name string) string {
	safe := unsafeFileNameChars.ReplaceAllString(name, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "file"
	}
	return safe
}

func mimeToExt(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func intPtr(v int) *int {
	return &v
}
