package controllers

import (
	"net/http"
	"strconv"

	"livesteam_app/controller/services"

	"github.com/gin-gonic/gin"
)

func ListLivestreamComments(c *gin.Context) {
	if !validID(c, "livestream_id") {
		return
	}
	var query struct {
		After uint64 `form:"after"`
	}
	if !bindListQuery(c, &query) {
		return
	}
	comments, hasMore, err := services.ListLivestreamComments(c.Request.Context(), c.Param("livestream_id"), query.After)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	cursor := query.After
	if len(comments) > 0 {
		cursor = comments[len(comments)-1].ID
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": comments, "next_cursor": strconv.FormatUint(cursor, 10), "has_more": hasMore})
}

func AddLivestreamComment(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	var input struct {
		Content string `json:"content" binding:"required"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_live_comment"})
		return
	}
	comment, err := services.AddLivestreamComment(c.Request.Context(), c.Param("livestream_id"), userID, c.GetString("username"), input.Content)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, comment)
}
