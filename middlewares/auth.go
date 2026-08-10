package middlewares

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"livesteam_app/config"
	"livesteam_app/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrNoJWTSecret = errors.New("JWT_SECRET not set")

func getJWTSecret() ([]byte, error) {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		return nil, ErrNoJWTSecret
	}
	return []byte(s), nil
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.DB == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Database not initialized"})
			return
		}

		var tokenString string

		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if tokenString == "" {
			tokenString = c.Query("token")
		}

		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization token missing"})
			return
		}

		secretKey, err := getJWTSecret()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "JWT secret not set"})
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return secretKey, nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			return
		}

		userIDStr, ok := claims["user_id"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID in token"})
			return
		}

		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid UUID format"})
			return
		}

		var user models.User
		if err := config.DB.First(&user, "id = ?", userIDStr).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			return
		}

		if !user.IsVerified {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User not verified"})
			return
		}

		if user.Status != "active" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User is not active"})
			return
		}

		c.Set("userID", userID)
		c.Set("username", user.Username)
		c.Set("user", user)

		c.Next()
	}
}
