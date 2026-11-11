package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func UploadFile(c *gin.Context, fieldName string) (string, error) {
	file, err := c.FormFile(fieldName)
	if err != nil {
		return "", fmt.Errorf("no file uploaded")
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".mp4": true}
	if !allowed[ext] {
		return "", fmt.Errorf("invalid file format")
	}

	filename := uuid.New().String() + ext
	os.MkdirAll("./uploads", os.ModePerm)
	savePath := filepath.Join("./uploads", filename)

	if err := c.SaveUploadedFile(file, savePath); err != nil {
		return "", fmt.Errorf("failed to save file")
	}

	return "/uploads/" + filename, nil
}
