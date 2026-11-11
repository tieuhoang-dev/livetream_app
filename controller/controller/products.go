package controllers

import (
	"encoding/json"
	"io"
	"net/http"

	"livesteam_app/controller/services"
	"livesteam_app/models"

	"github.com/gin-gonic/gin"
)

func bindProductJSON(c *gin.Context, input interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product request"})
		return false
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Expected a single JSON object"})
		return false
	}
	return true
}

// Do not serialize the associated User or its credential fields.
func productJSON(product models.Product) gin.H {
	return gin.H{
		"id": product.ID, "seller_id": product.SellerID, "name": product.Name,
		"price": product.Price, "image_url": product.ImageURL,
		"stock_quantity": product.StockQuantity, "description": product.Description,
		"created_at": product.CreatedAt,
	}
}

func CreateProduct(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var input services.CreateProductInput
	if !bindProductJSON(c, &input) {
		return
	}
	product, err := services.CreateProduct(userID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, productJSON(*product))
}

func ListProducts(c *gin.Context) {
	query := services.ProductQuery{Page: 1, PageSize: 20}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product query"})
		return
	}
	products, total, err := services.ListProducts(query)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items := make([]gin.H, 0, len(products))
	for _, product := range products {
		items = append(items, productJSON(product))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": query.Page, "page_size": query.PageSize})
}

func GetProduct(c *gin.Context) {
	if !validID(c, "product_id") {
		return
	}
	product, err := services.GetProduct(c.Param("product_id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, productJSON(*product))
}

func UpdateProduct(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "product_id") {
		return
	}
	var input services.UpdateProductInput
	if !bindProductJSON(c, &input) {
		return
	}
	product, err := services.UpdateProduct(userID, c.Param("product_id"), input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, productJSON(*product))
}

func AdjustProductStock(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "product_id") {
		return
	}
	var input struct {
		QuantityDelta int `json:"quantity_delta"`
	}
	if !bindProductJSON(c, &input) {
		return
	}
	product, err := services.AdjustProductStock(userID, c.Param("product_id"), input.QuantityDelta)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, productJSON(*product))
}

func DeleteProduct(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "product_id") {
		return
	}
	if err := services.DeleteProduct(userID, c.Param("product_id")); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
