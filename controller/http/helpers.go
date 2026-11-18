package controllers

import (
	"net/http"

	"livesteam_app/controller/services"
	"livesteam_app/internal/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func currentUserID(c *gin.Context) (string, bool) {
	value, exists := c.Get("userID")
	id, ok := value.(uuid.UUID)
	if !exists || !ok || id == uuid.Nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return "", false
	}
	return id.String(), true
}

func writeServiceError(c *gin.Context, err error) {
	status, message := apperrors.Public(err)
	c.JSON(status, gin.H{"error": message})
}

func validID(c *gin.Context, name string) bool {
	if id, err := uuid.Parse(c.Param(name)); err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid " + name})
		return false
	}
	return true
}

func defaultPagination() services.Pagination {
	return services.Pagination{Page: 1, PageSize: 20}
}

func bindListQuery(c *gin.Context, query interface{}) bool {
	if err := c.ShouldBindQuery(query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_query"})
		return false
	}
	return true
}

func writePage[T any](c *gin.Context, page services.Pagination, items []T, total int64) {
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page.Page, "page_size": page.PageSize})
}
