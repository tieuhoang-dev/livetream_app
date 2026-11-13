package services

import (
	"strings"
	"time"

	"livesteam_app/config"
	"livesteam_app/models"

	"gorm.io/gorm"
)

type LivestreamQuery struct {
	Pagination
	Search    string `form:"q" binding:"max=100"`
	SellerID  string `form:"seller_id" binding:"omitempty,uuid"`
	ProductID string `form:"product_id" binding:"omitempty,uuid"`
	Status    string `form:"status" binding:"required,oneof=start end all"`
	Sort      string `form:"sort" binding:"required,oneof=newest oldest"`
}

type LivestreamView struct {
	ID             string     `json:"id"`
	SellerID       string     `json:"seller_id"`
	SellerUsername string     `json:"seller_username"`
	SellerAvatar   string     `json:"seller_avatar"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
}

type LivestreamProductView struct {
	ID            string  `json:"id"`
	ProductID     string  `json:"product_id"`
	Name          string  `json:"name"`
	ImageURL      string  `json:"image_url"`
	Description   string  `json:"description"`
	OriginalPrice float64 `json:"original_price"`
	DiscountPrice float64 `json:"discount_price"`
	StockQuantity int     `json:"stock_quantity"`
	IsPinned      bool    `json:"is_pinned"`
}

func livestreamViewQuery(query *gorm.DB) *gorm.DB {
	return query.Joins("LEFT JOIN users ON users.id = livestreams.seller_id").Select(`
		livestreams.id, livestreams.seller_id, livestreams.title, livestreams.status,
		livestreams.started_at, livestreams.ended_at,
		COALESCE(users.username, '') AS seller_username, COALESCE(users.avatar, '') AS seller_avatar`)
}

func ListLivestreams(input LivestreamQuery) ([]LivestreamView, int64, error) {
	query := config.DB.Model(&models.Livestream{})
	if input.Status != "all" {
		query = query.Where("livestreams.status = ?", input.Status)
	}
	if input.SellerID != "" {
		query = query.Where("livestreams.seller_id = ?", input.SellerID)
	}
	if input.ProductID != "" {
		product := config.DB.Model(&models.LivestreamProduct{}).Select("1").
			Where("livestream_products.livestream_id = livestreams.id AND product_id = ?", input.ProductID)
		query = query.Where("EXISTS (?)", product)
	}
	if term := strings.TrimSpace(input.Search); term != "" {
		query = query.Where("livestreams.title LIKE ? ESCAPE '!'", containsPattern(term))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "livestreams.started_at DESC, livestreams.id DESC"
	if input.Sort == "oldest" {
		order = "livestreams.started_at ASC, livestreams.id ASC"
	}
	items := make([]LivestreamView, 0)
	err := livestreamViewQuery(query).Order(order).Offset(input.Offset()).Limit(input.PageSize).Find(&items).Error
	return items, total, err
}

func GetLivestream(id string) (*LivestreamView, error) {
	var live LivestreamView
	query := config.DB.Model(&models.Livestream{}).Where("livestreams.id = ?", id)
	if err := livestreamViewQuery(query).Take(&live).Error; err != nil {
		return nil, lookupError(err, "livestream_not_found")
	}
	return &live, nil
}

func ListLivestreamProducts(id string, page Pagination) ([]LivestreamProductView, int64, error) {
	if err := config.DB.Select("id").Take(&models.Livestream{}, "id = ?", id).Error; err != nil {
		return nil, 0, lookupError(err, "livestream_not_found")
	}
	query := config.DB.Model(&models.LivestreamProduct{}).Where("livestream_id = ?", id)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]LivestreamProductView, 0)
	err := query.Select(`id, product_id, product_name AS name, product_image AS image_url,
		description, price_at_purchase AS original_price, discount_price, stock_quantity, is_pinned`).
		Order("is_pinned DESC, id ASC").Offset(page.Offset()).Limit(page.PageSize).Find(&items).Error
	return items, total, err
}
