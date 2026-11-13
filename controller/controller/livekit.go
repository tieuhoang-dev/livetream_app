package controllers

import (
	"net/http"

	"livesteam_app/controller/services"

	"github.com/gin-gonic/gin"
)

func CreateLiveMediaSession(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	var input struct {
		Role string `json:"role" binding:"required,oneof=publisher viewer"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_media_role"})
		return
	}
	session, err := services.CreateLiveMediaSession(c.Request.Context(), c.Param("livestream_id"), userID, input.Role == "publisher")
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, session)
}

// Ending over HTTP avoids replacing an existing application WebSocket connection.
func EndLive(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	live, mediaClosed, err := services.EndLivestreamAndMedia(userID, c.Param("livestream_id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": live.ID, "title": live.Title, "status": live.Status, "ended_at": live.EndedAt, "media_closed": mediaClosed})
}
