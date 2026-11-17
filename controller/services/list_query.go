package services

import "strings"

type Pagination struct {
	Page     int `json:"page" form:"page" binding:"gte=1,lte=1000000"`
	PageSize int `json:"page_size" form:"page_size" binding:"gte=1,lte=100"`
}

func (p Pagination) Offset() int {
	return (p.Page - 1) * p.PageSize
}

func containsPattern(term string) string {
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(term)
	return "%" + escaped + "%"
}
