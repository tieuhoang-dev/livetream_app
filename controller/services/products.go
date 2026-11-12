package services

import (
	"errors"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"livesteam_app/config"
	"livesteam_app/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CreateProductInput struct {
	Name          string   `json:"name"`
	Price         *float64 `json:"price"`
	ImageURL      string   `json:"image_url"`
	StockQuantity *int     `json:"stock_quantity"`
	Description   string   `json:"description"`
}

type UpdateProductInput struct {
	Name        *string  `json:"name"`
	Price       *float64 `json:"price"`
	ImageURL    *string  `json:"image_url"`
	Description *string  `json:"description"`
}

type ProductQuery struct {
	Page     int    `form:"page" binding:"gte=1,lte=1000000"`
	PageSize int    `form:"page_size" binding:"gte=1,lte=100"`
	SellerID string `form:"seller_id" binding:"omitempty,uuid"`
	Search   string `form:"q" binding:"max=100"`
}

func productPrice(price float64) (float64, error) {
	// Match DECIMAL(10,2); reject values MySQL would round or overflow.
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 || price > 99999999.99 || math.Abs(price*100-math.Round(price*100)) > 0.000001 {
		return 0, errors.New("invalid_price")
	}
	return math.Round(price*100) / 100, nil
}

func validProductText(value string, required bool) bool {
	return (!required || value != "") && utf8.RuneCountInString(value) <= 255
}

func validProductImage(value string) bool {
	if !validProductText(value, false) {
		return false
	}
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return ((u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil) ||
		(u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/uploads/"))
}

func CreateProduct(userID string, input CreateProductInput) (*models.Product, error) {
	input.Name, input.ImageURL, input.Description = strings.TrimSpace(input.Name), strings.TrimSpace(input.ImageURL), strings.TrimSpace(input.Description)
	if !validProductText(input.Name, true) || !validProductText(input.Description, false) || !validProductImage(input.ImageURL) {
		return nil, errors.New("invalid_product")
	}
	if input.StockQuantity == nil || *input.StockQuantity < 0 {
		return nil, errors.New("invalid_quantity")
	}
	if input.Price == nil {
		return nil, errors.New("invalid_price")
	}
	price, err := productPrice(*input.Price)
	if err != nil {
		return nil, err
	}
	product := models.Product{
		ID: uuid.NewString(), SellerID: userID, Name: input.Name, Price: price,
		ImageURL: input.ImageURL, StockQuantity: *input.StockQuantity, Description: input.Description,
	}
	if err := config.DB.Create(&product).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

func ListProducts(input ProductQuery) ([]models.Product, int64, error) {
	if input.Page < 1 || input.Page > 1000000 || input.PageSize < 1 || input.PageSize > 100 || utf8.RuneCountInString(input.Search) > 100 {
		return nil, 0, errors.New("invalid_pagination")
	}
	query := config.DB.Model(&models.Product{})
	if input.SellerID != "" {
		query = query.Where("seller_id = ?", input.SellerID)
	}
	if term := strings.TrimSpace(input.Search); term != "" {
		term = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(term)
		query = query.Where("name LIKE ? ESCAPE '!'", "%"+term+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	products := make([]models.Product, 0)
	err := query.Order("created_at DESC, id DESC").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&products).Error
	return products, total, err
}

func GetProduct(productID string) (*models.Product, error) {
	var product models.Product
	if err := config.DB.First(&product, "id = ?", productID).Error; err != nil {
		return nil, lookupError(err, "product_not_found")
	}
	return &product, nil
}

func ownedProduct(tx *gorm.DB, userID, productID string) (*models.Product, error) {
	var product models.Product
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&product, "id = ?", productID).Error; err != nil {
		return nil, lookupError(err, "product_not_found")
	}
	if product.SellerID != userID {
		return nil, errors.New("forbidden")
	}
	return &product, nil
}

func UpdateProduct(userID, productID string, input UpdateProductInput) (*models.Product, error) {
	updates := make(map[string]interface{})
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if !validProductText(name, true) {
			return nil, errors.New("invalid_product")
		}
		updates["name"] = name
	}
	if input.Price != nil {
		price, err := productPrice(*input.Price)
		if err != nil {
			return nil, err
		}
		updates["price"] = price
	}
	if input.ImageURL != nil {
		image := strings.TrimSpace(*input.ImageURL)
		if !validProductImage(image) {
			return nil, errors.New("invalid_product")
		}
		updates["image_url"] = image
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if !validProductText(description, false) {
			return nil, errors.New("invalid_product")
		}
		updates["description"] = description
	}
	if len(updates) == 0 {
		return nil, errors.New("empty_update")
	}
	var product *models.Product
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		product, err = ownedProduct(tx, userID, productID)
		if err != nil {
			return err
		}
		// A map preserves explicit zero prices and empty optional fields.
		if err := tx.Model(product).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(product, "id = ?", productID).Error
	})
	return product, err
}

func AdjustProductStock(userID, productID string, delta int) (*models.Product, error) {
	if delta == 0 {
		return nil, errors.New("invalid_quantity")
	}
	var product *models.Product
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		product, err = ownedProduct(tx, userID, productID)
		if err != nil {
			return err
		}
		if delta < 0 && delta < -product.StockQuantity {
			return errors.New("insufficient_stock")
		}
		if delta > 0 && product.StockQuantity > int(^uint(0)>>1)-delta {
			return errors.New("invalid_quantity")
		}
		product.StockQuantity += delta
		return tx.Model(product).Update("stock_quantity", product.StockQuantity).Error
	})
	return product, err
}

func DeleteProduct(userID, productID string) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		product, err := ownedProduct(tx, userID, productID)
		if err != nil {
			return err
		}
		// Keep products referenced by live stock allocations or historical order
		// items, so removing a product cannot break restocking or order history.
		var references int64
		if err := tx.Model(&models.LivestreamProduct{}).Where("product_id = ?", productID).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return errors.New("product_in_use")
		}
		if err := tx.Model(&models.OrderItem{}).Where("product_id = ?", productID).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return errors.New("product_in_use")
		}
		return tx.Delete(product).Error
	})
}
