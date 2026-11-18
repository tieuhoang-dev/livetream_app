package utils

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const MaxImageBytes = 10 << 20

// UploadImage is shared by avatars and product photos. Never trust a client
// filename/extension or store executable formats under the public upload root.
func UploadImage(c *gin.Context, field, category string) (string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxImageBytes+(64<<10))
	if err := c.Request.ParseMultipartForm(MaxImageBytes); err != nil {
		return "", errors.New("An image file up to 10 MB is required")
	}
	defer c.Request.MultipartForm.RemoveAll()
	file, _, err := c.Request.FormFile(field)
	if err != nil {
		return "", errors.New("An image file up to 10 MB is required")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > MaxImageBytes {
		return "", errors.New("An image file up to 10 MB is required")
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[http.DetectContentType(data)]
	if ext == "" {
		return "", errors.New("Only JPEG, PNG or WebP images are supported")
	}
	directory := filepath.Join("uploads", category)
	if err := os.MkdirAll(directory, 0755); err != nil {
		log.Printf("Image storage: %v", err)
		return "", errors.New("Failed to save image")
	}
	path := filepath.Join(directory, uuid.NewString()+ext)
	if err := os.WriteFile(path, data, 0644); err != nil {
		os.Remove(path)
		log.Printf("Image storage: %v", err)
		return "", errors.New("Failed to save image")
	}
	return "/" + filepath.ToSlash(path), nil
}
