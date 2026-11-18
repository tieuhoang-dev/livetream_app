package middlewares

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"livesteam_app/config"
	"livesteam_app/models"
)

var ErrNoJWTSecret = errors.New("JWT_SECRET not set")

// Authenticate is shared by bearer HTTP requests and the WebSocket auth frame.
func Authenticate(ctx context.Context, tokenString string) (*models.User, time.Time, error) {
	if config.DB == nil {
		return nil, time.Time{}, errors.New("Database not initialized")
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, time.Time{}, ErrNoJWTSecret
	}
	token, err := jwt.Parse(tokenString, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, time.Time{}, errors.New("Invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, time.Time{}, errors.New("Invalid token claims")
	}
	userID, _ := claims["user_id"].(string)
	id, err := uuid.Parse(userID)
	if err != nil || id == uuid.Nil {
		return nil, time.Time{}, errors.New("Invalid user ID in token")
	}
	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil {
		return nil, time.Time{}, errors.New("Invalid token")
	}
	var user models.User
	if err := config.DB.WithContext(ctx).First(&user, "id = ?", id.String()).Error; err != nil {
		return nil, time.Time{}, errors.New("User not found")
	}
	if !user.IsVerified {
		return nil, time.Time{}, errors.New("User not verified")
	}
	if user.Status != "active" {
		return nil, time.Time{}, errors.New("User is not active")
	}
	return &user, expiry.Time, nil
}
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization token missing"})
			return
		}
		user, _, err := Authenticate(c.Request.Context(), token)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, ErrNoJWTSecret) {
				status = 500
			}
			c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
			return
		}
		c.Set("userID", uuid.MustParse(user.ID))
		c.Set("username", user.Username)
		c.Set("user", *user)
		c.Next()
	}
}
