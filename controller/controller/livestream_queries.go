package controllers

import (
	"net/http"

	"livesteam_app/controller/services"

	"github.com/gin-gonic/gin"
)

func ListLivestreams(c *gin.Context) {
	query := services.LivestreamQuery{Pagination: defaultPagination(), Status: "start", Sort: "newest"}
	if !bindListQuery(c, &query) {
		return
	}
	items, total, err := services.ListLivestreams(query)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writePage(c, query.Pagination, items, total)
}

func GetLivestream(c *gin.Context) {
	if !validID(c, "livestream_id") {
		return
	}
	live, err := services.GetLivestream(c.Param("livestream_id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, live)
}

func ListLivestreamProducts(c *gin.Context) {
	page := defaultPagination()
	if !validID(c, "livestream_id") || !bindListQuery(c, &page) {
		return
	}
	items, total, err := services.ListLivestreamProducts(c.Param("livestream_id"), page)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writePage(c, page, items, total)
}
