package routes_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"livesteam_app/config"
	controllers "livesteam_app/controller/controller"
	"livesteam_app/models"
	"livesteam_app/routes"
	"livesteam_app/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Use an explicitly supplied disposable MySQL database. Every fixture is scoped
// to fresh UUIDs and removed; no existing user, order, or product is changed.
func TestConnectedAPI(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run the HTTP/WebSocket integration suite")
	}
	t.Setenv("JWT_SECRET", "integration-test-secret-only")
	t.Setenv("BREVO_API_KEY", "")
	for _, key := range []string{"LIVEKIT_URL", "LIVEKIT_API_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		t.Setenv(key, "")
	}
	t.Chdir(t.TempDir())
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	models.MigrateAll(db)
	oldDB, oldControllerDB := config.DB, controllers.DB
	oldShipping := config.ShippingProvider
	carrier := &quoteFixture{}
	config.ShippingProvider = carrier
	config.DB = db
	controllers.SetDatabase(db)
	t.Cleanup(func() {
		config.DB = oldDB
		config.ShippingProvider = oldShipping
		controllers.SetDatabase(oldControllerDB)
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	if uri := os.Getenv("TEST_MONGO_URI"); uri != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Ping(ctx, nil); err != nil {
			t.Fatal(err)
		}
		collection := client.Database("live_comment_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]).Collection("chat_logs")
		oldCollection := config.ChatCollection
		config.ChatCollection = collection
		t.Cleanup(func() {
			config.ChatCollection = oldCollection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			collection.Database().Drop(ctx)
			client.Disconnect(ctx)
		})
		config.EnsureMongoIndexes()
	}
	password := "Local-test-password-123"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	newUser := func() models.User {
		id := uuid.NewString()
		u := models.User{ID: id, Username: id, Email: id + "@example.invalid", PasswordHash: string(hash), IsVerified: true, Status: "active", Role: "user"}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		return u
	}
	seller, buyer, other := newUser(), newUser(), newUser()
	ids := []string{seller.ID, buyer.ID, other.ID}
	deliveryFixture := gin.H{"name": "Test receiver", "phone": "0901234567", "province_code": "1", "ward": "Phường thử nghiệm", "address_line": "12 Đường thử nghiệm", "carrier_province_id": 1, "carrier_district_id": 100, "carrier_ward_code": "ward-100"}
	for _, uid := range ids {
		settings := models.ShippingSettings{SellerID: uid, Pickup: models.DeliveryAddress{Name: "Test shop", Phone: "0901234567", ProvinceCode: "1", ProvinceName: "Thành phố Hà Nội", Ward: "Phường shop", AddressLine: "34 Đường shop"}}
		settings.Pickup.CarrierProvinceID, settings.Pickup.CarrierDistrictID, settings.Pickup.CarrierWardCode = 1, 100, "ward-100"
		if err := db.Create(&settings).Error; err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM livestream_pickups WHERE livestream_id IN (SELECT id FROM livestreams WHERE seller_id IN ?)", ids)
		db.Exec("DELETE FROM order_deliveries WHERE order_id IN (SELECT id FROM orders WHERE user_id IN ?)", ids)
		db.Where("seller_id IN ?", ids).Delete(&models.ShippingSettings{})
		db.Exec("DELETE FROM payments WHERE order_id IN (SELECT id FROM orders WHERE user_id IN ?)", ids)
		db.Exec("DELETE FROM order_items WHERE order_id IN (SELECT id FROM orders WHERE user_id IN ?)", ids)
		db.Where("user_id IN ?", ids).Delete(&models.Order{})
		db.Exec("DELETE FROM livestream_products WHERE livestream_id IN (SELECT id FROM livestreams WHERE seller_id IN ?)", ids)
		db.Where("seller_id IN ?", ids).Delete(&models.Livestream{})
		db.Where("seller_id IN ?", ids).Delete(&models.Product{})
		db.Where("id IN ?", ids).Delete(&models.User{})
	})
	tokens := map[string]string{}
	for _, u := range []models.User{seller, buyer, other} {
		token, err := utils.GenerateJWT(u.ID, u.Role, u.Username)
		if err != nil {
			t.Fatal(err)
		}
		tokens[u.ID] = token
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Static("/uploads", "./uploads")
	routes.SetupUserRoutes(r)
	server := httptest.NewServer(r)
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	call := func(t *testing.T, actor, method, path string, payload interface{}, want int) map[string]interface{} {
		t.Helper()
		if values, ok := payload.(gin.H); ok {
			copy := gin.H{}
			for k, v := range values {
				copy[k] = v
			}
			for _, field := range []string{"delivery", "pickup_address"} {
				if address, ok := copy[field].(gin.H); ok {
					cloned := gin.H{}
					for k, v := range address {
						cloned[k] = v
					}
					province := 1
					if fmt.Sprint(cloned["province_code"]) == "79" {
						province = 79
					}
					if _, exists := cloned["carrier_province_id"]; !exists {
						cloned["carrier_province_id"] = province
						cloned["carrier_district_id"] = province * 100
						cloned["carrier_ward_code"] = fmt.Sprintf("ward-%d", province*100)
					}
					copy[field] = cloned
				}
			}
			payload = copy
		}
		if method == "POST" && (path == "/api/orders" || path == "/api/orders/quote") {
			if values, ok := payload.(gin.H); ok {
				cloned := gin.H{}
				for key, value := range values {
					cloned[key] = value
				}
				if _, ok := cloned["delivery"]; !ok {
					cloned["delivery"] = deliveryFixture
				}
				payload = cloned
			}
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		if actor != "" {
			req.Header.Set("Authorization", "Bearer "+tokens[actor])
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, res.StatusCode, body)
		}
		var out map[string]interface{}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	stock := func(t *testing.T, productID string) int {
		t.Helper()
		var p models.Product
		if err := db.First(&p, "id = ?", productID).Error; err != nil {
			t.Fatal(err)
		}
		return p.StockQuantity
	}
	product := func(t *testing.T, owner string, quantity int) models.Product {
		t.Helper()
		out := call(t, owner, "POST", "/api/products", gin.H{"name": "Test product", "price": 100, "stock_quantity": quantity, "image_url": "/uploads/test.png", "description": "Fixture", "parcel": models.ProductParcel{WeightGrams: 500, LengthCM: 20, WidthCM: 15, HeightCM: 10}}, 201)
		data, _ := json.Marshal(out)
		var p models.Product
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	order := func(t *testing.T, p models.Product, liveID *string, qty int) string {
		t.Helper()
		out := call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": qty, "livestream_id": liveID, "payment_method": "manual"}, 201)
		return out["order_id"].(string)
	}

	t.Run("protected routes require JWT", func(t *testing.T) {
		for _, endpoint := range [][2]string{
			{"GET", "/api/shipping/status"}, {"GET", "/api/shipping/provinces"}, {"GET", "/api/shipping/districts?province_id=1"}, {"GET", "/api/shipping/wards?district_id=100"},
			{"POST", "/api/products"}, {"PATCH", "/api/products/" + uuid.NewString()},
			{"DELETE", "/api/products/" + uuid.NewString()}, {"POST", "/api/products/" + uuid.NewString() + "/stock"},
			{"PUT", "/api/users/me"}, {"POST", "/api/users/me/avatar"}, {"POST", "/api/orders"},
			{"GET", "/api/orders"}, {"GET", "/api/orders/" + uuid.NewString()},
			{"GET", "/api/sales/offline"}, {"GET", "/api/sales/offline/" + uuid.NewString()},
			{"GET", "/api/sales/orders"}, {"GET", "/api/sales/orders/" + uuid.NewString()},
			{"POST", "/api/sales/orders/" + uuid.NewString() + "/pay"},
			{"POST", "/api/sales/orders/" + uuid.NewString() + "/refund"},
			{"POST", "/api/orders/" + uuid.NewString() + "/pay"}, {"POST", "/api/orders/" + uuid.NewString() + "/cancel"},
			{"POST", "/api/orders/" + uuid.NewString() + "/refund"},
			{"POST", "/api/livestreams/" + uuid.NewString() + "/products"},
			{"DELETE", "/api/livestreams/" + uuid.NewString() + "/products/" + uuid.NewString()},
		} {
			call(t, "", endpoint[0], endpoint[1], nil, 401)
		}
	})

	t.Run("product catalog CRUD, pagination and owner permissions", func(t *testing.T) {
		p := product(t, seller.ID, 5)
		second := product(t, seller.ID, 0)
		foreign := product(t, buyer.ID, 1)
		if p.SellerID != seller.ID || p.CreatedAt.IsZero() {
			t.Fatal("product owner or creation timestamp missing")
		}
		out := call(t, "", "GET", "/api/products/"+p.ID, nil, 200)
		for _, forbidden := range []string{"user", "password_hash", "verification_code_hash", "price_at_purchase"} {
			if _, exists := out[forbidden]; exists {
				t.Fatalf("product exposes %s", forbidden)
			}
		}
		firstPage := call(t, "", "GET", "/api/products?seller_id="+seller.ID+"&page=1&page_size=1", nil, 200)
		secondPage := call(t, "", "GET", "/api/products?seller_id="+seller.ID+"&page=2&page_size=1", nil, 200)
		firstItems, secondItems := firstPage["data"].([]interface{}), secondPage["data"].([]interface{})
		if firstPage["total"] != float64(2) || len(firstItems) != 1 || len(secondItems) != 1 || firstItems[0].(map[string]interface{})["id"] == secondItems[0].(map[string]interface{})["id"] {
			t.Fatal("seller filtering or pagination failed")
		}
		empty := call(t, "", "GET", "/api/products?q="+uuid.NewString(), nil, 200)
		if empty["total"] != float64(0) || len(empty["data"].([]interface{})) != 0 {
			t.Fatal("empty catalog query must return an empty array")
		}
		for _, path := range []string{"/api/products?page=0", "/api/products?page_size=101", "/api/products?seller_id=invalid", "/api/products/not-a-uuid"} {
			call(t, "", "GET", path, nil, 400)
		}
		call(t, "", "GET", "/api/products/"+uuid.NewString(), nil, 404)
		call(t, buyer.ID, "PATCH", "/api/products/"+p.ID, gin.H{"name": "Hijacked"}, 403)
		call(t, buyer.ID, "POST", "/api/products/"+p.ID+"/stock", gin.H{"quantity_delta": 1}, 403)
		call(t, buyer.ID, "DELETE", "/api/products/"+p.ID, nil, 403)
		call(t, seller.ID, "PATCH", "/api/products/"+uuid.NewString(), gin.H{"name": "Missing"}, 404)
		call(t, seller.ID, "PATCH", "/api/products/"+p.ID, gin.H{}, 400)
		call(t, seller.ID, "PATCH", "/api/products/"+p.ID, gin.H{"stock_quantity": 1000}, 400)
		updated := call(t, seller.ID, "PATCH", "/api/products/"+p.ID, gin.H{"name": "  A 100%_product!  ", "price": 0, "description": "", "image_url": ""}, 200)
		if updated["name"] != "A 100%_product!" || updated["price"] != float64(0) || updated["description"] != "" || updated["image_url"] != "" || updated["stock_quantity"] != float64(5) {
			t.Fatal("partial update did not preserve explicit zero/empty fields or stock")
		}
		literal := call(t, "", "GET", "/api/products?seller_id="+seller.ID+"&q=100%25_product!", nil, 200)
		if literal["total"] != float64(1) {
			t.Fatal("literal search with SQL wildcard characters failed")
		}
		for _, payload := range []gin.H{
			{"name": " ", "price": 10, "stock_quantity": 1},
			{"name": "Product", "price": -1, "stock_quantity": 1},
			{"name": "Product", "price": 1.001, "stock_quantity": 1},
			{"name": "Product", "price": 100000000, "stock_quantity": 1},
			{"name": "Product", "stock_quantity": 1},
			{"name": "Product", "price": 1},
			{"name": "Product", "price": 1, "stock_quantity": -1},
			{"name": "Product", "price": 1, "stock_quantity": 1, "image_url": "javascript:alert(1)"},
			{"name": "Product", "price": 1, "stock_quantity": 1, "seller_id": buyer.ID},
		} {
			call(t, seller.ID, "POST", "/api/products", payload, 400)
		}
		call(t, seller.ID, "DELETE", "/api/products/"+p.ID, nil, 204)
		call(t, "", "GET", "/api/products/"+p.ID, nil, 404)
		call(t, seller.ID, "DELETE", "/api/products/"+p.ID, nil, 404)
		call(t, seller.ID, "DELETE", "/api/products/"+second.ID, nil, 204)
		call(t, buyer.ID, "DELETE", "/api/products/"+foreign.ID, nil, 204)
	})

	t.Run("stock adjustments are atomic and cannot underflow or overflow", func(t *testing.T) {
		p := product(t, seller.ID, 0)
		codes := make(chan int, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req, _ := http.NewRequest("POST", server.URL+"/api/products/"+p.ID+"/stock", strings.NewReader(`{"quantity_delta":1}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+tokens[seller.ID])
				res, err := client.Do(req)
				if err != nil {
					codes <- 0
					return
				}
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				codes <- res.StatusCode
			}()
		}
		wg.Wait()
		close(codes)
		for code := range codes {
			if code != 200 {
				t.Fatalf("unexpected stock adjustment response %d", code)
			}
		}
		if stock(t, p.ID) != 8 {
			t.Fatal("concurrent stock increments were lost")
		}
		call(t, seller.ID, "POST", "/api/products/"+p.ID+"/stock", gin.H{"quantity_delta": -9}, 409)
		call(t, seller.ID, "POST", "/api/products/"+p.ID+"/stock", gin.H{"quantity_delta": 0}, 400)
		call(t, seller.ID, "POST", "/api/products/"+p.ID+"/stock", gin.H{"quantity_delta": int(^uint(0) >> 1)}, 400)
		call(t, seller.ID, "POST", "/api/products/"+p.ID+"/stock", gin.H{"quantity_delta": -8}, 200)
		if stock(t, p.ID) != 0 {
			t.Fatal("failed adjustments changed stock")
		}
	})

	t.Run("profile and downloadable avatar", func(t *testing.T) {
		call(t, buyer.ID, "PUT", "/api/users/me", gin.H{"fullname": "Buyer Test", "avatar": "/test.png"}, 200)
		var updated models.User
		db.First(&updated, "id = ?", buyer.ID)
		if updated.Fullname != "Buyer Test" {
			t.Fatal("profile did not use the UUID from JWT")
		}
		image, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j1ioAAAAASUVORK5CYII=")
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", "avatar.png")
		part.Write(image)
		form.Close()
		req, _ := http.NewRequest("POST", server.URL+"/api/users/me/avatar", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+tokens[buyer.ID])
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]string
		json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 || !strings.HasPrefix(out["url"], "/uploads/") {
			t.Fatalf("avatar response: %d %v", res.StatusCode, out)
		}
		download, err := client.Get(server.URL + out["url"])
		if err != nil {
			t.Fatal(err)
		}
		defer download.Body.Close()
		data, _ := io.ReadAll(download.Body)
		if download.StatusCode != 200 || !bytes.Equal(data, image) {
			t.Fatal("uploaded avatar cannot be downloaded")
		}
		db.First(&updated, "id = ?", buyer.ID)
		if updated.Avatar != out["url"] {
			t.Fatal("avatar URL not persisted for authenticated user")
		}
	})

	t.Run("order ownership, state transitions and stock", func(t *testing.T) {
		p := product(t, seller.ID, 5)
		call(t, buyer.ID, "POST", "/api/orders/not-a-uuid/pay", gin.H{"payment_method": "manual"}, 400)
		call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": 1}, 400)
		call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": -1, "payment_method": "manual"}, 400)
		call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": 6, "payment_method": "manual"}, 409)
		call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": uuid.NewString(), "quantity": 1, "payment_method": "manual"}, 404)
		call(t, seller.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": 1, "payment_method": "manual"}, 403)
		id := order(t, p, nil, 2)
		call(t, seller.ID, "DELETE", "/api/products/"+p.ID, nil, 409)
		call(t, seller.ID, "PATCH", "/api/products/"+p.ID, gin.H{"price": 120}, 200)
		if stock(t, p.ID) != 3 {
			t.Fatal("stock was not deducted")
		}
		purchase := "/api/orders/" + id
		sale := "/api/sales/orders/" + id
		paymentInput := gin.H{"payment_method": "manual"}
		refundInput := gin.H{"refund_reason": "Test refund"}
		call(t, seller.ID, "POST", purchase+"/cancel", nil, 404)
		call(t, other.ID, "GET", sale, nil, 404)
		call(t, seller.ID, "GET", sale, nil, 200)
		for _, path := range []string{sale, purchase} {
			call(t, buyer.ID, "POST", path+"/pay", paymentInput, 403)
			call(t, other.ID, "POST", path+"/pay", paymentInput, 404)
		}
		before := call(t, buyer.ID, "GET", purchase, nil, 200)
		if before["status"] != "pending" || before["payment"].(map[string]interface{})["status"] != "pending" || stock(t, p.ID) != 3 {
			t.Fatal("denied payments changed the order, payment, or stock")
		}
		call(t, seller.ID, "POST", sale+"/pay", paymentInput, 200)
		call(t, seller.ID, "POST", sale+"/pay", paymentInput, 409)
		call(t, buyer.ID, "POST", purchase+"/cancel", nil, 409)
		for _, path := range []string{sale, purchase} {
			call(t, buyer.ID, "POST", path+"/refund", refundInput, 403)
			call(t, other.ID, "POST", path+"/refund", refundInput, 404)
		}
		before = call(t, buyer.ID, "GET", purchase, nil, 200)
		if before["status"] != "paid" || before["payment"].(map[string]interface{})["status"] != "paid" || stock(t, p.ID) != 3 {
			t.Fatal("denied refunds changed the order, payment, or stock")
		}
		call(t, seller.ID, "POST", sale+"/refund", gin.H{"refund_reason": "   "}, 400)
		call(t, seller.ID, "POST", sale+"/refund", refundInput, 200)
		call(t, seller.ID, "POST", sale+"/refund", refundInput, 409)
		var payment models.Payment
		db.First(&payment, "order_id = ?", id)
		if stock(t, p.ID) != 5 || payment.Status != "refunded" || payment.Amount != 200 {
			t.Fatal("refund did not restore stock once and synchronize payment")
		}
		call(t, seller.ID, "DELETE", "/api/products/"+p.ID, nil, 409)
		cancelID := order(t, p, nil, 1)
		call(t, buyer.ID, "POST", "/api/orders/"+cancelID+"/cancel", nil, 200)
		call(t, buyer.ID, "POST", "/api/orders/"+cancelID+"/cancel", nil, 409)
		call(t, seller.ID, "POST", "/api/sales/orders/"+cancelID+"/pay", paymentInput, 409)
		if stock(t, p.ID) != 5 {
			t.Fatal("repeated cancellation changed stock")
		}
		if err := db.Model(&models.Order{}).Where("id = ?", cancelID).Updates(map[string]interface{}{"user_id": seller.ID, "status": "pending"}).Error; err != nil {
			t.Fatal(err)
		}
		call(t, seller.ID, "POST", "/api/sales/orders/"+cancelID+"/pay", paymentInput, 403)
	})

	t.Run("failed cancellation rolls back order and stock", func(t *testing.T) {
		p := product(t, seller.ID, 1)
		id := order(t, p, nil, 1)
		if err := db.Where("order_id = ?", id).Delete(&models.Payment{}).Error; err != nil {
			t.Fatal(err)
		}
		call(t, buyer.ID, "POST", "/api/orders/"+id+"/cancel", nil, 404)
		var stored models.Order
		db.First(&stored, "id = ?", id)
		if stored.Status != "pending" || stock(t, p.ID) != 0 {
			t.Fatal("failed transaction left a changed order or stock")
		}
	})

	t.Run("buyer cancellation and seller payment cannot both succeed", func(t *testing.T) {
		p := product(t, seller.ID, 1)
		id := order(t, p, nil, 1)
		type result struct {
			action string
			code   int
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		goAction := func(actor, path, action, body string) {
			<-start
			req, err := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
			if err != nil {
				results <- result{action, 0}
				return
			}
			req.Header.Set("Authorization", "Bearer "+tokens[actor])
			req.Header.Set("Content-Type", "application/json")
			res, err := client.Do(req)
			if err != nil {
				results <- result{action, 0}
				return
			}
			defer res.Body.Close()
			results <- result{action, res.StatusCode}
		}
		go goAction(seller.ID, "/api/sales/orders/"+id+"/pay", "paid", `{"payment_method":"manual"}`)
		go goAction(buyer.ID, "/api/orders/"+id+"/cancel", "cancelled", "")
		close(start)
		first, second := <-results, <-results
		if first.code != 200 {
			first, second = second, first
		}
		if first.code != 200 || second.code != 409 {
			t.Fatalf("unexpected concurrent results: %v / %v", first, second)
		}
		detail := call(t, buyer.ID, "GET", "/api/orders/"+id, nil, 200)
		wantStock := 0
		if first.action == "cancelled" {
			wantStock = 1
		}
		if detail["status"] != first.action || detail["payment"].(map[string]interface{})["status"] != first.action || stock(t, p.ID) != wantStock {
			t.Fatalf("order, payment, and stock disagree after concurrent updates: %v", detail)
		}
	})

	t.Run("concurrent orders cannot oversell", func(t *testing.T) {
		p := product(t, seller.ID, 1)
		codes := make(chan int, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				encoded, _ := json.Marshal(gin.H{"product_id": p.ID, "quantity": 1, "payment_method": "manual", "delivery": deliveryFixture})
				payload := string(encoded)
				req, _ := http.NewRequest("POST", server.URL+"/api/orders", strings.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+tokens[buyer.ID])
				res, err := client.Do(req)
				if err != nil {
					codes <- 0
					return
				}
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				codes <- res.StatusCode
			}()
		}
		wg.Wait()
		close(codes)
		created := 0
		for code := range codes {
			if code == 201 {
				created++
			} else if code != 409 {
				t.Fatalf("unexpected concurrent response %d", code)
			}
		}
		if created != 1 || stock(t, p.ID) != 0 {
			t.Fatalf("created=%d stock=%d", created, stock(t, p.ID))
		}
	})

	t.Run("public websocket live lists validate filters and push lifecycle changes", func(t *testing.T) {
		baseWS := "ws" + strings.TrimPrefix(server.URL, "http")
		dial := func(path string) *websocket.Conn {
			t.Helper()
			conn, _, err := websocket.DefaultDialer.Dial(baseWS+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close() })
			return conn
		}
		auth := dial("/api/auth/ws?token=" + tokens[seller.ID])
		first, second := dial("/api/livestreams/ws"), dial("/api/livestreams/ws")
		send := func(conn *websocket.Conn, message gin.H) {
			t.Helper()
			if err := conn.WriteJSON(message); err != nil {
				t.Fatal(err)
			}
		}
		read := func(conn *websocket.Conn, accept func(map[string]interface{}) bool) map[string]interface{} {
			t.Helper()
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			for {
				var reply map[string]interface{}
				if err := conn.ReadJSON(&reply); err != nil {
					t.Fatal(err)
				}
				if accept(reply) {
					return reply
				}
			}
		}
		page := func(conn *websocket.Conn, requestID string, total int) map[string]interface{} {
			t.Helper()
			return read(conn, func(r map[string]interface{}) bool {
				return r["type"] == "list_lives_success" && r["request_id"] == requestID && r["total"] == float64(total)
			})
		}
		command := func(kind, content string) map[string]interface{} {
			t.Helper()
			send(auth, gin.H{"type": kind, "content": content})
			return read(auth, func(r map[string]interface{}) bool { return r["type"] == kind+"_success" })
		}
		prefix := "WS list " + uuid.NewString()
		send(first, gin.H{"type": "list_lives", "request_id": "all", "query": gin.H{"q": prefix, "seller_id": seller.ID, "status": "all", "sort": "oldest", "page_size": 1}})
		send(second, gin.H{"type": "list_lives", "request_id": "open", "query": gin.H{"q": prefix}})
		page(first, "all", 0)
		page(second, "open", 0)
		idA := command("create_live", prefix+"_A")["id"].(string)
		created := page(first, "all", 1)
		page(second, "open", 1)
		entry := created["data"].([]interface{})[0].(map[string]interface{})
		if entry["id"] != idA || entry["seller_username"] != seller.Username || created["page_size"] != float64(1) {
			t.Fatalf("wrong live snapshot: %v", created)
		}
		for _, secret := range []string{"email", "password_hash", "user", "stream_url"} {
			if _, leaked := entry[secret]; leaked {
				t.Fatalf("public list leaked %s", secret)
			}
		}
		db.Model(&models.Livestream{}).Where("id = ?", idA).Update("started_at", time.Now().Add(-time.Hour))
		idB := command("create_live", prefix+"XA")["id"].(string)
		ordered := page(first, "all", 2)
		if ordered["data"].([]interface{})[0].(map[string]interface{})["id"] != idA {
			t.Fatalf("oldest sort not applied: %v", ordered)
		}
		page(second, "open", 2)
		send(first, gin.H{"type": "list_lives", "request_id": "literal", "query": gin.H{"q": prefix + "_A"}})
		page(first, "literal", 1)
		for _, invalid := range []gin.H{{"page": 0}, {"page_size": 101}, {"page_size": "20"}, {"status": "bad"}, {"sort": "price"}, {"seller_id": "bad"}, {"product_id": "bad"}, {"q": strings.Repeat("a", 101)}} {
			send(first, gin.H{"type": "list_lives", "request_id": "invalid", "query": invalid})
			reply := read(first, func(r map[string]interface{}) bool { return r["request_id"] == "invalid" })
			if reply["type"] != "list_lives_error" || reply["error"] != "invalid_query" {
				t.Fatalf("accepted invalid filter %v: %v", invalid, reply)
			}
		}
		// This public endpoint must never execute authenticated commands.
		send(first, gin.H{"type": "create_live", "request_id": "readonly", "content": "Forbidden"})
		denied := read(first, func(r map[string]interface{}) bool { return r["request_id"] == "readonly" })
		if denied["type"] != "list_lives_error" {
			t.Fatal("public socket accepted a write")
		}
		p := product(t, seller.ID, 10)
		send(first, gin.H{"type": "list_lives", "request_id": "product", "query": gin.H{"seller_id": seller.ID, "product_id": p.ID}})
		page(first, "product", 0)
		call(t, seller.ID, "POST", "/api/livestreams/"+idA+"/products", gin.H{"product_id": p.ID, "discount_price": 80, "stock_quantity": 2}, 201)
		page(first, "product", 1)
		call(t, seller.ID, "DELETE", "/api/livestreams/"+idA+"/products/"+p.ID, nil, 204)
		page(first, "product", 0)
		command("end_live", idA)
		page(second, "open", 1)
		call(t, seller.ID, "POST", "/api/livestreams/"+idB+"/end", nil, 200)
		page(second, "open", 0)
		send(first, gin.H{"type": "list_lives", "request_id": "page2", "query": gin.H{"q": prefix, "status": "end", "sort": "oldest", "page": 2, "page_size": 1}})
		ended := page(first, "page2", 2)
		if ended["page"] != float64(2) || ended["data"].([]interface{})[0].(map[string]interface{})["id"] != idB {
			t.Fatalf("wrong ended page: %v", ended)
		}
		first.Close()
		reconnected := dial("/api/livestreams/ws")
		send(reconnected, gin.H{"type": "list_lives", "request_id": "again", "query": gin.H{"q": prefix, "status": "end"}})
		page(reconnected, "again", 2)
	})

	t.Run("create live and manage its products", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/auth/ws?token="+tokens[seller.ID], nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		ws.SetReadDeadline(time.Now().Add(10 * time.Second))
		ws.WriteJSON(gin.H{"type": "create_live", "content": "API integration live"})
		var response map[string]interface{}
		if err := ws.ReadJSON(&response); err != nil {
			t.Fatal(err)
		}
		if response["type"] != "create_live_success" {
			t.Fatalf("create_live: %v", response)
		}
		liveID := response["id"].(string)
		assertJoin := func(id, want string) {
			t.Helper()
			if err := ws.WriteJSON(gin.H{"type": "join_live", "content": id}); err != nil {
				t.Fatal(err)
			}
			var reply map[string]interface{}
			if err := ws.ReadJSON(&reply); err != nil {
				t.Fatal(err)
			}
			if reply["type"] != want {
				t.Fatalf("join_live: want %s, got %v", want, reply)
			}
		}
		assertJoin(liveID, "join_live_success")
		assertJoin(uuid.NewString(), "join_live_error")
		var live models.Livestream
		db.First(&live, "id = ?", liveID)
		if live.EndedAt != nil {
			t.Fatal("new live must have NULL ended_at")
		}
		p := product(t, seller.ID, 20)
		foreign := product(t, buyer.ID, 5)
		path := "/api/livestreams/" + liveID + "/products"
		payload := gin.H{"product_id": p.ID, "discount_price": 80, "stock_quantity": 10, "is_pinned": true}
		call(t, seller.ID, "POST", path, gin.H{"product_id": p.ID, "discount_price": -1, "stock_quantity": 1}, 400)
		call(t, seller.ID, "POST", path, gin.H{"product_id": p.ID, "discount_price": 80, "stock_quantity": 0}, 400)
		call(t, buyer.ID, "POST", path, payload, 403)
		call(t, seller.ID, "POST", path, gin.H{"product_id": foreign.ID, "discount_price": 80, "stock_quantity": 1}, 403)
		call(t, seller.ID, "POST", path, payload, 201)
		call(t, seller.ID, "DELETE", "/api/products/"+p.ID, nil, 409)
		call(t, seller.ID, "PATCH", "/api/products/"+p.ID, gin.H{"price": 110, "name": "Updated catalog name"}, 200)
		var attached models.LivestreamProduct
		db.First(&attached, "livestream_id = ? AND product_id = ?", liveID, p.ID)
		if attached.ProductName != p.Name || attached.ProductImage != p.ImageURL || attached.Description != p.Description || !attached.IsPinned {
			t.Fatal("live product metadata was not copied")
		}
		if attached.DiscountPrice != 80 || attached.PriceAtPurchase != 100 {
			t.Fatal("editing catalog product changed live price snapshot")
		}
		call(t, seller.ID, "POST", path, payload, 409)
		if stock(t, p.ID) != 10 {
			t.Fatal("live stock was not reserved from ordinary stock")
		}
		id := order(t, p, &liveID, 2)
		var payment models.Payment
		db.First(&payment, "order_id = ?", id)
		if payment.Amount != 160 {
			t.Fatal("live order did not use discount price")
		}
		call(t, buyer.ID, "DELETE", path+"/"+p.ID, nil, 403)
		call(t, seller.ID, "DELETE", path+"/"+p.ID, nil, 409)
		call(t, buyer.ID, "POST", "/api/orders/"+id+"/cancel", nil, 200)
		paidID := order(t, p, &liveID, 3)
		call(t, seller.ID, "POST", "/api/sales/orders/"+paidID+"/pay", gin.H{"payment_method": "manual"}, 200)
		call(t, seller.ID, "POST", "/api/sales/orders/"+paidID+"/refund", gin.H{"refund_reason": "Test"}, 200)
		call(t, seller.ID, "DELETE", path+"/"+p.ID, nil, 204)
		if stock(t, p.ID) != 20 {
			t.Fatal("removal did not return unsold live allocation")
		}
		ws.WriteJSON(gin.H{"type": "end_live", "content": liveID})
		if err := ws.ReadJSON(&response); err != nil || response["type"] != "end_live_success" {
			t.Fatalf("end_live: %v %v", response, err)
		}
		assertJoin(liveID, "join_live_error")
		call(t, seller.ID, "POST", path, payload, 409)
		call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": 1, "livestream_id": liveID, "payment_method": "manual"}, 409)
	})

	t.Run("discover livestreams and reconcile live and offline sales", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/auth/ws?token="+tokens[seller.ID], nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		if err := ws.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		liveMessage := func(kind, content string) map[string]interface{} {
			t.Helper()
			if err := ws.WriteJSON(gin.H{"type": kind, "content": content}); err != nil {
				t.Fatal(err)
			}
			var reply map[string]interface{}
			if err := ws.ReadJSON(&reply); err != nil || reply["type"] != kind+"_success" {
				t.Fatalf("%s: %v %v", kind, reply, err)
			}
			return reply
		}
		prefix := "Live " + uuid.NewString()
		liveID := liveMessage("create_live", prefix+"_A")["id"].(string)
		otherID := liveMessage("create_live", prefix+"XA")["id"].(string)
		if err := db.Model(&models.Livestream{}).Where("id = ?", liveID).Update("started_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		query := "/api/livestreams?seller_id=" + seller.ID + "&q=" + url.QueryEscape(prefix)
		list := call(t, "", "GET", query+"&sort=oldest&page_size=1", nil, 200)
		if list["total"] != float64(2) || list["data"].([]interface{})[0].(map[string]interface{})["id"] != liveID {
			t.Fatalf("oldest first: %v", list)
		}
		list = call(t, "", "GET", query+"&page_size=1", nil, 200)
		if list["data"].([]interface{})[0].(map[string]interface{})["id"] != otherID {
			t.Fatalf("newest first: %v", list)
		}
		list = call(t, "", "GET", query+"&sort=oldest&page_size=1&page=2", nil, 200)
		if list["data"].([]interface{})[0].(map[string]interface{})["id"] != otherID {
			t.Fatalf("second page: %v", list)
		}
		literalQuery := "/api/livestreams?q=" + url.QueryEscape(prefix+"_A")
		if list := call(t, "", "GET", literalQuery, nil, 200); list["total"] != float64(1) {
			t.Fatalf("underscore in title must match literally: %v", list)
		}
		for _, filter := range []string{"status=unknown", "sort=price", "seller_id=bad", "product_id=bad", "page_size=101"} {
			call(t, "", "GET", "/api/livestreams?"+filter, nil, 400)
		}
		livePath := "/api/livestreams/" + liveID
		live := call(t, "", "GET", livePath, nil, 200)
		if live["seller_username"] != seller.Username || live["status"] != "start" || live["ended_at"] != nil || live["user"] != nil {
			t.Fatalf("unexpected public live detail: %v", live)
		}
		call(t, "", "GET", "/api/livestreams/"+uuid.NewString()+"/products", nil, 404)
		if list := call(t, "", "GET", livePath+"/products", nil, 200); len(list["data"].([]interface{})) != 0 {
			t.Fatalf("expected empty live product list: %v", list)
		}
		p := product(t, seller.ID, 10)
		call(t, seller.ID, "POST", livePath+"/products", gin.H{"product_id": p.ID, "discount_price": 80, "stock_quantity": 10, "is_pinned": true}, 201)
		catalog := "/api/products?seller_id=" + seller.ID + "&in_stock=true&page_size=100"
		containsProduct := func(list map[string]interface{}) bool {
			for _, row := range list["data"].([]interface{}) {
				if row.(map[string]interface{})["id"] == p.ID {
					return true
				}
			}
			return false
		}
		if containsProduct(call(t, "", "GET", catalog, nil, 200)) {
			t.Fatal("stock reserved for live must not appear in the in-stock catalog")
		}
		if list := call(t, "", "GET", query+"&product_id="+p.ID, nil, 200); list["total"] != float64(1) {
			t.Fatalf("product filter: %v", list)
		}
		list = call(t, "", "GET", livePath+"/products", nil, 200)
		item := list["data"].([]interface{})[0].(map[string]interface{})
		if item["product_id"] != p.ID || item["original_price"] != float64(100) || item["discount_price"] != float64(80) || item["is_pinned"] != true {
			t.Fatalf("live product snapshot: %v", item)
		}
		paidID := order(t, p, &liveID, 2)
		call(t, seller.ID, "POST", "/api/sales/orders/"+paidID+"/pay", gin.H{"payment_method": "manual"}, 200)
		pendingID := order(t, p, &liveID, 3)
		liveMessage("end_live", liveID)
		endedAt := call(t, "", "GET", livePath, nil, 200)["ended_at"]
		liveMessage("end_live", liveID)
		if stock(t, p.ID) != 5 || endedAt == nil || call(t, "", "GET", livePath, nil, 200)["ended_at"] != endedAt {
			t.Fatal("ending live must return free stock once and preserve ended_at")
		}
		if list := call(t, "", "GET", literalQuery, nil, 200); list["total"] != float64(0) {
			t.Fatalf("ended live must not appear in default list: %v", list)
		}
		for _, status := range []string{"end", "all"} {
			if list := call(t, "", "GET", literalQuery+"&status="+status, nil, 200); list["total"] != float64(1) {
				t.Fatalf("status %s: %v", status, list)
			}
		}
		if !containsProduct(call(t, "", "GET", catalog, nil, 200)) {
			t.Fatal("stock returned by end_live must be visible in the offline catalog")
		}
		offline := call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": 4, "payment_method": "manual"}, 201)
		offlineID := offline["order_id"].(string)
		if offline["total_amount"] != float64(400) || stock(t, p.ID) != 1 {
			t.Fatalf("offline purchase must use catalog price and stock: %v", offline)
		}
		call(t, buyer.ID, "POST", "/api/orders/"+pendingID+"/cancel", nil, 200)
		call(t, buyer.ID, "POST", "/api/sales/orders/"+paidID+"/refund", gin.H{"refund_reason": "Test refund"}, 403)
		call(t, seller.ID, "POST", "/api/sales/orders/"+paidID+"/refund", gin.H{"refund_reason": "Test refund"}, 200)
		if stock(t, p.ID) != 6 {
			t.Fatal("live cancellation and refund after end must restore catalog stock")
		}
		assertOrders := func(actor, path, wantID, wantStatus string, offlineOnly bool) {
			t.Helper()
			list := call(t, actor, "GET", path, nil, 200)
			found := false
			for _, row := range list["data"].([]interface{}) {
				entry := row.(map[string]interface{})
				if entry["status"] != wantStatus || (entry["livestream_id"] == nil) != offlineOnly {
					t.Fatalf("wrong order for %s: %v", path, entry)
				}
				found = found || entry["id"] == wantID
			}
			if !found {
				t.Fatalf("expected order %s in %s: %v", wantID, path, list)
			}
		}
		assertOrders(buyer.ID, "/api/orders?channel=offline&status=pending", offlineID, "pending", true)
		assertOrders(buyer.ID, "/api/orders?channel=live&status=refunded", paidID, "refunded", false)
		assertOrders(seller.ID, "/api/sales/offline?status=pending", offlineID, "pending", true)
		assertOrders(seller.ID, "/api/sales/orders?channel=live&status=refunded", paidID, "refunded", false)
		assertOrders(seller.ID, "/api/sales/orders?channel=offline&status=pending", offlineID, "pending", true)
		call(t, seller.ID, "GET", "/api/sales/orders/"+paidID, nil, 200)
		call(t, buyer.ID, "GET", "/api/sales/orders/"+paidID, nil, 404)
		call(t, seller.ID, "GET", "/api/sales/orders?channel=unknown", nil, 400)
		if list := call(t, buyer.ID, "GET", "/api/sales/offline", nil, 200); list["total"] != float64(0) {
			t.Fatalf("buyer received another seller's sales: %v", list)
		}
		call(t, buyer.ID, "GET", "/api/orders?channel=unknown", nil, 400)
		call(t, seller.ID, "GET", "/api/sales/offline?status=unknown", nil, 400)
		for _, actorPath := range [][2]string{{buyer.ID, "/api/orders/"}, {seller.ID, "/api/sales/offline/"}} {
			detail := call(t, actorPath[0], "GET", actorPath[1]+offlineID, nil, 200)
			line := detail["items"].([]interface{})[0].(map[string]interface{})
			payment := detail["payment"].(map[string]interface{})
			if detail["livestream_id"] != nil || line["product_id"] != p.ID || line["name"] != p.Name || line["quantity"] != float64(4) || line["price_at_purchase"] != float64(100) || payment["payment_method"] != "manual" {
				t.Fatalf("offline order detail: %v", detail)
			}
		}
		call(t, buyer.ID, "GET", "/api/sales/offline/"+offlineID, nil, 404)
		call(t, seller.ID, "GET", "/api/orders/"+offlineID, nil, 404)
		call(t, seller.ID, "GET", "/api/sales/offline/"+paidID, nil, 404)
		call(t, seller.ID, "DELETE", livePath+"/products/"+p.ID, nil, 204)
		if stock(t, p.ID) != 6 {
			t.Fatal("removing an ended allocation must not restore stock again")
		}
	})

	t.Run("comments are shared within one livestream", func(t *testing.T) {
		if os.Getenv("TEST_MONGO_URI") == "" {
			t.Skip("set TEST_MONGO_URI for Mongo comment HTTP tests")
		}
		newLive := func() string {
			t.Helper()
			live := models.Livestream{ID: uuid.NewString(), SellerID: seller.ID, Title: "Comments test", Status: "start", StartedAt: time.Now()}
			if err := db.Create(&live).Error; err != nil {
				t.Fatal(err)
			}
			return "/api/livestreams/" + live.ID
		}
		live := newLive()
		path := live + "/comments"
		call(t, "", "GET", path, nil, 401)
		call(t, "", "POST", path, gin.H{"content": "Hi"}, 401)
		call(t, buyer.ID, "GET", path+"?after=-1", nil, 400)
		call(t, buyer.ID, "POST", path, gin.H{"content": "   "}, 400)
		call(t, buyer.ID, "POST", path, gin.H{"content": strings.Repeat("ệ", 301)}, 400)
		first := call(t, buyer.ID, "POST", path, gin.H{"content": "  Hàng còn không?  ", "username": "fake"}, 201)
		if first["content"] != "Hàng còn không?" || first["username"] != buyer.Username || first["id"] == "" {
			t.Fatalf("unexpected comment: %v", first)
		}
		all := call(t, seller.ID, "GET", path, nil, 200)
		if len(all["data"].([]interface{})) != 1 || all["next_cursor"] != first["id"] || all["has_more"] != false {
			t.Fatalf("unexpected comment history: %v", all)
		}
		second := call(t, seller.ID, "POST", path, gin.H{"content": "Còn hàng nhé"}, 201)
		next := call(t, buyer.ID, "GET", path+"?after="+first["id"].(string), nil, 200)
		if len(next["data"].([]interface{})) != 1 || next["next_cursor"] != second["id"] {
			t.Fatalf("unexpected incremental result: %v", next)
		}
		other := call(t, buyer.ID, "GET", newLive()+"/comments", nil, 200)
		if len(other["data"].([]interface{})) != 0 {
			t.Fatalf("comments leaked to another room: %v", other)
		}
		call(t, seller.ID, "POST", live+"/end", nil, 200)
		call(t, buyer.ID, "POST", path, gin.H{"content": "Kết thúc rồi?"}, 409)
		afterEnd := call(t, buyer.ID, "GET", path, nil, 200)
		if len(afterEnd["data"].([]interface{})) != 2 {
			t.Fatalf("comment history after end: %v", afterEnd)
		}
		var doc models.LivestreamComment
		if err := config.ChatCollection.FindOne(context.Background(), bson.M{"_id": "live-comment:" + strings.TrimPrefix(live, "/api/livestreams/") + ":" + first["id"].(string)}).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		if doc.Content != first["content"] || doc.MessageType != "livestream_comment" {
			t.Fatal("HTTP response returned before Mongo persisted the comment")
		}
		if db.Migrator().HasTable("livestream_comments") {
			t.Fatal("HTTP comments created SQL content storage")
		}

	})

	t.Run("LiveKit grants and ending a room", func(t *testing.T) {
		live := models.Livestream{ID: uuid.NewString(), SellerID: seller.ID, Title: "Video permissions", Status: "start", StartedAt: time.Now()}
		if err := db.Create(&live).Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/livestreams/" + live.ID
		publisher := gin.H{"role": "publisher"}
		viewer := gin.H{"role": "viewer"}
		call(t, "", "POST", path+"/media-token", viewer, 401)
		call(t, buyer.ID, "POST", path+"/media-token", publisher, 403)
		call(t, buyer.ID, "POST", path+"/media-token", gin.H{"role": "admin"}, 400)
		call(t, buyer.ID, "POST", path+"/media-token", viewer, 503)
		const secret = "isolated-livekit-test-secret"
		parse := func(raw string) (jwt.MapClaims, error) {
			token, err := jwt.Parse(raw, func(*jwt.Token) (interface{}, error) { return []byte(secret), nil }, jwt.WithIssuer("testkey"), jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
			if err != nil {
				return nil, err
			}
			return token.Claims.(jwt.MapClaims), nil
		}
		cleanupRequests := make(chan string, 2)
		media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Room string `json:"room"`
			}
			claims, err := parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if err != nil || r.Method != http.MethodPost || r.URL.Path != "/twirp/livekit.RoomService/DeleteRoom" || json.NewDecoder(r.Body).Decode(&input) != nil {
				http.Error(w, "unexpected room request", 400)
				return
			}
			grant, _ := claims["video"].(map[string]interface{})
			if grant["roomCreate"] != true || grant["roomJoin"] != nil {
				http.Error(w, "invalid admin grant", 403)
				return
			}
			cleanupRequests <- input.Room
			w.WriteHeader(http.StatusOK)
		}))
		defer media.Close()
		t.Setenv("LIVEKIT_URL", "wss://video.example.invalid")
		t.Setenv("LIVEKIT_API_URL", media.URL)
		t.Setenv("LIVEKIT_API_KEY", "testkey")
		t.Setenv("LIVEKIT_API_SECRET", secret)
		receive := func(user string, input gin.H, publish bool) string {
			t.Helper()
			result := call(t, user, "POST", path+"/media-token", input, 200)
			claims, err := parse(result["token"].(string))
			if err != nil {
				t.Fatal(err)
			}
			grant := claims["video"].(map[string]interface{})
			if result["url"] != "wss://video.example.invalid" || grant["room"] != "livemart-"+live.ID || grant["roomJoin"] != true || grant["canPublish"] != publish || grant["canSubscribe"] == publish || grant["canPublishData"] != false || grant["roomCreate"] != nil {
				t.Fatalf("unexpected media grant: %v", grant)
			}
			if publish && fmt.Sprint(grant["canPublishSources"]) != "[camera microphone]" {
				t.Fatalf("unexpected sources: %v", grant)
			}
			return claims["sub"].(string)
		}
		if receive(seller.ID, publisher, true) != "seller-"+seller.ID {
			t.Fatal("publisher identity changed")
		}
		if receive(buyer.ID, viewer, false) == receive(buyer.ID, viewer, false) {
			t.Fatal("viewer tabs must have independent identities")
		}
		call(t, buyer.ID, "POST", path+"/end", nil, 403)
		result := call(t, seller.ID, "POST", path+"/end", nil, 200)
		if result["status"] != "end" || result["media_closed"] != true || <-cleanupRequests != "livemart-"+live.ID {
			t.Fatalf("unexpected end response: %v", result)
		}
		call(t, seller.ID, "POST", path+"/media-token", publisher, 409)
		call(t, buyer.ID, "POST", path+"/media-token", viewer, 409)
	})

	t.Run("checkout uses seller origin and snapshots delivery fees", func(t *testing.T) {
		carrier.same, carrier.other = 25000, 40000
		defer func() { carrier.same, carrier.other = 0, 0 }()
		pickup := gin.H{"name": "Pickup contact", "phone": "0901234567", "province_code": "1", "ward": "Phường shop", "address_line": "Shop address"}
		body := gin.H{"pickup_address": pickup, "same_province_fee": 25000, "other_province_fee": 40000}
		call(t, seller.ID, "PUT", "/api/sales/shipping-settings", body, 200)
		t.Cleanup(func() {
			db.Model(&models.ShippingSettings{}).Where("seller_id = ?", seller.ID).Updates(map[string]interface{}{"same_province_fee": 0, "other_province_fee": 0, "free_shipping_from": nil})
		})
		p := product(t, seller.ID, 20)
		requestID := uuid.NewString()
		pinned := gin.H{}
		for key, value := range deliveryFixture {
			pinned[key] = value
		}
		pinned["latitude"] = 21.028511
		pinned["longitude"] = 105.804817
		input := gin.H{"product_id": p.ID, "quantity": 2, "payment_method": "cod", "delivery": pinned, "request_id": requestID, "note": "Call first"}
		quote := call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		if quote["items_subtotal"] != float64(200) || quote["shipping_fee"] != float64(25000) || quote["total_amount"] != float64(25200) {
			t.Fatal("wrong local shipping quote", quote)
		}
		wrong := gin.H{}
		for key, value := range input {
			wrong[key] = value
		}
		wrong["expected_total"] = 1
		call(t, buyer.ID, "POST", "/api/orders", wrong, 409)
		if stock(t, p.ID) != 20 {
			t.Fatal("stale quote deducted stock")
		}
		input["expected_total"] = quote["total_amount"]
		placed := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		again := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		if placed["order_id"] != again["order_id"] || stock(t, p.ID) != 18 {
			t.Fatal("idempotent checkout duplicated order/stock")
		}
		input["quantity"] = 3
		call(t, buyer.ID, "POST", "/api/orders", input, 409)
		input["quantity"] = 2
		detail := call(t, buyer.ID, "GET", "/api/orders/"+placed["order_id"].(string), nil, 200)
		if detail["payment"].(map[string]interface{})["amount"] != float64(25200) {
			t.Fatal("payment excluded shipping")
		}
		recipient := detail["delivery"].(map[string]interface{})["recipient"].(map[string]interface{})
		if recipient["name"] != "Test receiver" || recipient["address_line"] != "12 Đường thử nghiệm" || recipient["latitude"] != 21.028511 || recipient["longitude"] != 105.804817 {
			t.Fatal("delivery snapshot missing")
		}
		body["same_province_fee"] = 90000
		pickup["address_line"] = "New shop"
		call(t, seller.ID, "PUT", "/api/sales/shipping-settings", body, 200)
		saved := call(t, buyer.ID, "GET", "/api/orders/"+placed["order_id"].(string), nil, 200)
		if saved["shipping_fee"] != float64(25000) || saved["delivery"].(map[string]interface{})["pickup"].(map[string]interface{})["address_line"] != "Shop address" {
			t.Fatal("shop changes mutated old order")
		}
		remote := gin.H{}
		for key, value := range deliveryFixture {
			remote[key] = value
		}
		remote["province_code"] = "79"
		remote["carrier_province_id"], remote["carrier_district_id"], remote["carrier_ward_code"] = 79, 7900, "ward-7900"
		input["delivery"] = remote
		input["request_id"] = uuid.NewString()
		delete(input, "expected_total")
		remoteQuote := call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		if remoteQuote["shipping_fee"] != float64(40000) {
			t.Fatal("wrong inter-province fee", remoteQuote)
		}
		body["free_shipping_from"] = 100
		call(t, seller.ID, "PUT", "/api/sales/shipping-settings", body, 200)
		free := call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		if free["shipping_fee"] != float64(40000) {
			t.Fatal("seller setting changed the carrier quote")
		}
		call(t, other.ID, "GET", "/api/orders/"+placed["order_id"].(string), nil, 404)
		call(t, seller.ID, "GET", "/api/sales/orders/"+placed["order_id"].(string), nil, 200)
	})

	t.Run("station drop-off quotes canonical origin without creating a shipment", func(t *testing.T) {
		lookup := gin.H{"latitude": 21.028511, "longitude": 105.804817, "carrier_province_id": 1, "carrier_district_id": 100}
		call(t, "", "POST", "/api/shipping/stations/nearby", lookup, 401)
		call(t, seller.ID, "POST", "/api/shipping/stations/nearby", gin.H{"latitude": 91, "longitude": 105}, 400)
		nearby := call(t, seller.ID, "POST", "/api/shipping/stations/nearby", lookup, 200)
		if nearby["quote_only"] != true || len(nearby["data"].([]interface{})) != 1 {
			t.Fatal("missing station results", nearby)
		}
		live := models.Livestream{ID: uuid.NewString(), SellerID: seller.ID, Title: "Station origin", Status: "start", StartedAt: time.Now()}
		if err := db.Create(&live).Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/livestreams/" + live.ID
		p := product(t, seller.ID, 10)
		call(t, seller.ID, "POST", path+"/products", gin.H{"product_id": p.ID, "discount_price": 80, "stock_quantity": 3}, 201)
		pickup := gin.H{"station_id": 1888, "station_name": "Forged", "name": "Shop contact", "phone": "0901234567", "carrier_province_id": 1, "carrier_district_id": 100, "address_line": "Forged address"}
		saved := call(t, seller.ID, "PUT", path+"/pickup", gin.H{"pickup_address": pickup}, 200)
		canonical := saved["pickup_address"].(map[string]interface{})
		if canonical["address_line"] != "10 Đường bưu cục" || canonical["station_name"] != "Bưu cục thử nghiệm" || canonical["carrier_ward_code"] != "ward-100" {
			t.Fatal("station data trusted the browser", canonical)
		}
		call(t, buyer.ID, "PUT", path+"/pickup", gin.H{"pickup_address": pickup}, 403)
		pickup["station_id"] = 9999
		call(t, seller.ID, "PUT", path+"/pickup", gin.H{"pickup_address": pickup}, 400)
		input := gin.H{"product_id": p.ID, "quantity": 1, "livestream_id": live.ID, "payment_method": "cod", "delivery": deliveryFixture, "request_id": uuid.NewString()}
		quote := call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		meta := quote["shipping"].(map[string]interface{})
		if meta["origin_type"] != "station_dropoff" || meta["station_id"] != float64(1888) || carrier.lastQuote.Load().From.WardCode != "ward-100" {
			t.Fatal("quote did not use the canonical station route", quote)
		}
		input["expected_total"] = quote["total_amount"]
		placed := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		detail := call(t, buyer.ID, "GET", "/api/orders/"+placed["order_id"].(string), nil, 200)
		if detail["delivery"].(map[string]interface{})["pickup"].(map[string]interface{})["station_id"] != float64(1888) {
			t.Fatal("station snapshot missing")
		}
		carrier.stationFailure = errors.New("shipping_stations_access_required")
		defer func() { carrier.stationFailure = nil }()
		again := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		if again["order_id"] != placed["order_id"] {
			t.Fatal("retry failed while carrier station API was unavailable")
		}
		input["request_id"] = uuid.NewString()
		call(t, buyer.ID, "POST", "/api/orders", input, 503)
	})

	t.Run("live pickup uses broadcast origin and keeps order snapshots private", func(t *testing.T) {
		carrier.same, carrier.other = 25000, 40000
		defer func() { carrier.same, carrier.other = 0, 0 }()
		call(t, "", "POST", "/api/delivery/reverse-geocode", gin.H{"latitude": 21, "longitude": 105}, 401)
		call(t, buyer.ID, "POST", "/api/delivery/reverse-geocode", gin.H{"latitude": 91, "longitude": 105}, 400)
		call(t, buyer.ID, "POST", "/api/delivery/reverse-geocode", gin.H{"latitude": 21}, 400)
		live := models.Livestream{ID: uuid.NewString(), SellerID: seller.ID, Title: "Live pickup location", Status: "start", StartedAt: time.Now()}
		if err := db.Create(&live).Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/livestreams/" + live.ID
		call(t, "", "GET", path+"/pickup", nil, 401)
		call(t, buyer.ID, "GET", path+"/pickup", nil, 403)
		fallback := call(t, seller.ID, "GET", path+"/pickup", nil, 200)
		if fallback["pickup_address"].(map[string]interface{})["province_code"] != "1" {
			t.Fatalf("missing shop fallback: %v", fallback)
		}
		pickup := gin.H{"name": "Live seller", "phone": "0901234567", "province_code": "79", "ward": "Phường Sài Gòn", "address_line": "12 Nguyễn Huệ", "latitude": 10.773, "longitude": 106.704, "location_mode": "current"}
		call(t, buyer.ID, "PUT", path+"/pickup", gin.H{"pickup_address": pickup}, 403)
		call(t, seller.ID, "PUT", path+"/pickup", gin.H{"pickup_address": pickup}, 200)
		public := call(t, "", "GET", path, nil, 200)
		if public["pickup_address"] != nil || public["latitude"] != nil {
			t.Fatal("public live leaked pickup location")
		}
		db.Model(&models.ShippingSettings{}).Where("seller_id = ?", seller.ID).Updates(map[string]interface{}{"same_province_fee": 25000, "other_province_fee": 40000})
		defer db.Model(&models.ShippingSettings{}).Where("seller_id = ?", seller.ID).Updates(map[string]interface{}{"same_province_fee": 0, "other_province_fee": 0})
		p := product(t, seller.ID, 10)
		call(t, seller.ID, "POST", path+"/products", gin.H{"product_id": p.ID, "discount_price": 80, "stock_quantity": 2}, 201)
		destination := gin.H{"name": "Receiver", "phone": "0901234567", "province_code": "79", "ward": "Phường Sài Gòn", "address_line": "34 Nguyễn Huệ", "latitude": 10.774, "longitude": 106.705, "location_mode": "current"}
		input := gin.H{"product_id": p.ID, "quantity": 1, "livestream_id": live.ID, "payment_method": "cod", "delivery": destination, "request_id": uuid.NewString()}
		quote := call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		if quote["shipping_fee"] != float64(25000) || quote["pickup_province"] != "Thành phố Hồ Chí Minh" {
			t.Fatalf("used shop instead of live origin: %v", quote)
		}
		input["expected_total"] = quote["total_amount"]
		created := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		detailPath := "/api/orders/" + created["order_id"].(string)
		detail := call(t, buyer.ID, "GET", detailPath, nil, 200)
		address := detail["delivery"].(map[string]interface{})["pickup"].(map[string]interface{})
		if address["province_code"] != "79" || address["latitude"] != 10.773 || address["location_mode"] != "current" {
			t.Fatalf("wrong pickup snapshot: %v", address)
		}
		pickup["province_code"], pickup["location_mode"] = "1", "manual"
		pickup["latitude"], pickup["longitude"] = nil, nil
		call(t, seller.ID, "PUT", path+"/pickup", gin.H{"pickup_address": pickup}, 200)
		quote = call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		if quote["shipping_fee"] != float64(40000) {
			t.Fatalf("live pickup edit ignored: %v", quote)
		}
		detail = call(t, buyer.ID, "GET", detailPath, nil, 200)
		if detail["delivery"].(map[string]interface{})["pickup"].(map[string]interface{})["province_code"] != "79" {
			t.Fatal("live pickup edit mutated an existing order")
		}
		settings := call(t, seller.ID, "GET", "/api/sales/shipping-settings", nil, 200)
		if settings["settings"].(map[string]interface{})["pickup_address"].(map[string]interface{})["province_code"] != "1" {
			t.Fatal("live pickup changed shop address")
		}
		destination["latitude"] = nil
		call(t, buyer.ID, "POST", "/api/orders/quote", input, 400)
		call(t, seller.ID, "POST", path+"/end", nil, 200)
		call(t, seller.ID, "PUT", path+"/pickup", gin.H{"pickup_address": pickup}, 409)
	})

	t.Run("seller origin saves without seller fees and carrier quotes stay independent", func(t *testing.T) {
		carrier.same, carrier.other = 25000, 40000
		defer func() { carrier.same, carrier.other = 0, 0 }()
		db.Where("seller_id = ?", other.ID).Delete(&models.ShippingSettings{})
		live := models.Livestream{ID: uuid.NewString(), SellerID: other.ID, Title: "Carrier quote only", Status: "start", StartedAt: time.Now()}
		if err := db.Create(&live).Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/livestreams/" + live.ID
		call(t, other.ID, "PUT", path+"/pickup", gin.H{"pickup_address": deliveryFixture}, 200)
		p := product(t, other.ID, 10)
		call(t, other.ID, "POST", path+"/products", gin.H{"product_id": p.ID, "discount_price": 80, "stock_quantity": 2}, 201)
		area := gin.H{"province_code": "1", "carrier_province_id": 1, "carrier_district_id": 100, "carrier_ward_code": "ward-100"}
		input := gin.H{"product_id": p.ID, "quantity": 1, "livestream_id": live.ID, "delivery": area}
		quote := call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		if quote["shipping_fee"] != float64(25000) || quote["shipping"].(map[string]interface{})["quote_only"] != true {
			t.Fatal("not a carrier-only quote", quote)
		}
		input["payment_method"] = "cod"
		call(t, buyer.ID, "POST", "/api/orders", input, 400)
		input["delivery"], input["expected_total"] = deliveryFixture, quote["total_amount"]
		created := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		detail := call(t, buyer.ID, "GET", "/api/orders/"+created["order_id"].(string), nil, 200)
		shipping := detail["delivery"].(map[string]interface{})["shipping"].(map[string]interface{})
		if shipping["provider"] != "ghn" || shipping["environment"] != "staging" || shipping["quote_only"] != true || detail["payment"].(map[string]interface{})["amount"] != float64(25080) {
			t.Fatal("missing quote snapshot", detail)
		}
		call(t, other.ID, "PUT", path+"/pickup", gin.H{"pickup_address": deliveryFixture, "shipping_rates": gin.H{"same_province_fee": 0, "other_province_fee": 0}}, 200)
		quote = call(t, buyer.ID, "POST", "/api/orders/quote", input, 200)
		if quote["shipping_fee"] != float64(25000) {
			t.Fatal("seller overwrote carrier fee")
		}
	})

	t.Run("carrier status catalogs and route validation", func(t *testing.T) {
		status := call(t, buyer.ID, "GET", "/api/shipping/status", nil, 200)
		if status["quote_only"] != true || status["environment"] != "staging" || status["token"] != nil {
			t.Fatal("unsafe provider status", status)
		}
		provinces := call(t, buyer.ID, "GET", "/api/shipping/provinces", nil, 200)
		if len(provinces["data"].([]interface{})) != 2 {
			t.Fatal(provinces)
		}
		call(t, buyer.ID, "GET", "/api/shipping/districts?province_id=1", nil, 200)
		call(t, buyer.ID, "GET", "/api/shipping/wards?district_id=100", nil, 200)
		call(t, buyer.ID, "GET", "/api/shipping/districts?province_id=bad", nil, 400)
		p := product(t, seller.ID, 4)
		input := gin.H{"product_id": p.ID, "quantity": 1, "delivery": gin.H{"province_code": "1", "carrier_province_id": 79, "carrier_district_id": 7900, "carrier_ward_code": "ward-7900"}}
		before := carrier.calls.Load()
		call(t, buyer.ID, "POST", "/api/orders/quote", input, 400)
		input["delivery"] = gin.H{"province_code": "1", "carrier_province_id": 1, "carrier_district_id": 999, "carrier_ward_code": "ward-100"}
		call(t, buyer.ID, "POST", "/api/orders/quote", input, 400)
		input["delivery"] = gin.H{"province_code": "1", "carrier_province_id": 1, "carrier_district_id": 100, "carrier_ward_code": "fake"}
		call(t, buyer.ID, "POST", "/api/orders/quote", input, 400)
		if carrier.calls.Load() != before || stock(t, p.ID) != 4 {
			t.Fatal("invalid route quoted or changed stock")
		}
	})

	t.Run("carrier missing configuration or failure never produces a free order", func(t *testing.T) {
		p := product(t, seller.ID, 3)
		input := gin.H{"product_id": p.ID, "quantity": 1, "payment_method": "cod", "delivery": deliveryFixture}
		config.ShippingProvider = nil
		t.Setenv("GHN_TOKEN", "")
		t.Setenv("GHN_SHOP_ID", "")
		defer func() { config.ShippingProvider = carrier; carrier.failure = nil }()
		status := call(t, buyer.ID, "GET", "/api/shipping/status", nil, 200)
		if status["configured"] != false {
			t.Fatal("missing credentials reported configured")
		}
		call(t, buyer.ID, "POST", "/api/orders/quote", input, 503)
		call(t, buyer.ID, "POST", "/api/orders", input, 503)
		config.ShippingProvider = carrier
		carrier.failure = errors.New("shipping_unavailable")
		call(t, buyer.ID, "POST", "/api/orders/quote", input, 502)
		call(t, buyer.ID, "POST", "/api/orders", input, 502)
		if stock(t, p.ID) != 3 {
			t.Fatal("failed quote deducted stock")
		}
	})

	t.Run("parcel validation and quote snapshot changes are checked before mutation", func(t *testing.T) {
		p := product(t, seller.ID, 5)
		call(t, seller.ID, "PATCH", "/api/products/"+p.ID, gin.H{"parcel": gin.H{"weight_grams": 0, "length_cm": 20, "width_cm": 15, "height_cm": 10}}, 400)
		call(t, buyer.ID, "PATCH", "/api/products/"+p.ID, gin.H{"parcel": gin.H{"weight_grams": 600, "length_cm": 20, "width_cm": 15, "height_cm": 10}}, 403)
		input := gin.H{"product_id": p.ID, "quantity": 1, "payment_method": "cod", "delivery": deliveryFixture}
		db.Model(&models.Product{}).Where("id = ?", p.ID).Update("parcel_weight_grams", 0)
		call(t, buyer.ID, "POST", "/api/orders/quote", input, 409)
		db.Model(&models.Product{}).Where("id = ?", p.ID).Update("parcel_weight_grams", 500)
		carrier.beforeQuote = func() { db.Model(&models.Product{}).Where("id = ?", p.ID).Update("price", 101) }
		defer func() { carrier.beforeQuote = nil }()
		call(t, buyer.ID, "POST", "/api/orders", input, 409)
		if stock(t, p.ID) != 5 {
			t.Fatal("changed quote mutated stock")
		}
	})

	t.Run("idempotent local order retry does not call the carrier again", func(t *testing.T) {
		p := product(t, seller.ID, 2)
		input := gin.H{"product_id": p.ID, "quantity": 1, "payment_method": "cod", "delivery": deliveryFixture, "request_id": uuid.NewString()}
		created := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		before := carrier.calls.Load()
		carrier.failure = errors.New("shipping_unavailable")
		defer func() { carrier.failure = nil }()
		again := call(t, buyer.ID, "POST", "/api/orders", input, 201)
		if created["order_id"] != again["order_id"] || carrier.calls.Load() != before || stock(t, p.ID) != 1 {
			t.Fatal("retry duplicated carrier call/order")
		}
	})

	t.Run("concurrent retries create one delivery order", func(t *testing.T) {
		p := product(t, seller.ID, 4)
		encoded, _ := json.Marshal(gin.H{"product_id": p.ID, "quantity": 2, "payment_method": "cod", "delivery": deliveryFixture, "request_id": uuid.NewString()})
		type result struct {
			code int
			id   string
		}
		results := make(chan result, 8)
		var wait sync.WaitGroup
		for i := 0; i < 8; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				req, _ := http.NewRequest("POST", server.URL+"/api/orders", bytes.NewReader(encoded))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+tokens[buyer.ID])
				response, err := client.Do(req)
				if err != nil {
					results <- result{}
					return
				}
				defer response.Body.Close()
				var body map[string]interface{}
				json.NewDecoder(response.Body).Decode(&body)
				id, _ := body["order_id"].(string)
				results <- result{response.StatusCode, id}
			}()
		}
		wait.Wait()
		close(results)
		first := ""
		for result := range results {
			if result.code != 201 || result.id == "" {
				t.Fatal("concurrent retry failed", result)
			}
			if first == "" {
				first = result.id
			}
			if first != result.id {
				t.Fatal("concurrent request created multiple orders")
			}
		}
		if stock(t, p.ID) != 2 {
			t.Fatal("concurrent retries deducted stock multiple times")
		}
	})

	t.Run("delivery validation and shipping policy ownership", func(t *testing.T) {
		p := product(t, seller.ID, 3)
		base := gin.H{"product_id": p.ID, "quantity": 1, "payment_method": "cod", "delivery": nil}
		call(t, buyer.ID, "POST", "/api/orders", base, 400)
		bad := gin.H{}
		for key, value := range deliveryFixture {
			bad[key] = value
		}
		bad["latitude"] = 91
		bad["longitude"] = 105
		base["delivery"] = bad
		call(t, buyer.ID, "POST", "/api/orders", base, 400)
		bad["latitude"] = 21
		delete(bad, "longitude")
		call(t, buyer.ID, "POST", "/api/orders", base, 400)
		bad["latitude"] = nil
		bad["phone"] = "abc"
		call(t, buyer.ID, "POST", "/api/orders", base, 400)
		base["delivery"] = deliveryFixture
		base["payment_method"] = "fake-wallet"
		call(t, buyer.ID, "POST", "/api/orders", base, 400)
		call(t, buyer.ID, "PUT", "/api/sales/shipping-settings", gin.H{"pickup_address": deliveryFixture, "same_province_fee": -1, "other_province_fee": 0}, 200)
		own := call(t, buyer.ID, "GET", "/api/sales/shipping-settings", nil, 200)
		if !own["configured"].(bool) {
			t.Fatal("own shipping settings not loaded")
		}
		db.Where("seller_id = ?", other.ID).Delete(&models.ShippingSettings{})
		otherProduct := product(t, other.ID, 2)
		call(t, buyer.ID, "POST", "/api/orders/quote", gin.H{"product_id": otherProduct.ID, "quantity": 1, "payment_method": "cod"}, 409)
		if stock(t, p.ID) != 3 {
			t.Fatal("invalid delivery changed stock")
		}
	})

	t.Run("password reset uses a separate expiring single-use OTP", func(t *testing.T) {
		call(t, "", "POST", "/api/users/password/reset-request", gin.H{"email": "missing-" + uuid.NewString() + "@example.invalid"}, 404)
		call(t, "", "POST", "/api/users/password/reset-request", gin.H{"email": buyer.Email}, 502)
		var before models.User
		db.First(&before, "id = ?", buyer.ID)
		if before.PasswordResetCodeHash == "" || before.PasswordResetExpiresAt == nil {
			t.Fatal("reset request did not save an expiring hashed OTP")
		}
		otpHash, _ := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.MinCost)
		db.Model(&models.User{}).Where("id = ?", buyer.ID).Updates(map[string]interface{}{"password_reset_code_hash": string(otpHash), "password_reset_expires_at": time.Now().Add(-time.Minute)})
		payload := gin.H{"email": buyer.Email, "code": "123456", "new_password": "New-local-test-password"}
		call(t, "", "POST", "/api/users/password/reset", payload, 400)
		db.Model(&models.User{}).Where("id = ?", buyer.ID).Update("password_reset_expires_at", time.Now().Add(time.Minute))
		call(t, "", "POST", "/api/users/password/reset", gin.H{"email": buyer.Email, "code": "000000", "new_password": "New-local-test-password"}, 400)
		call(t, "", "POST", "/api/users/verify", gin.H{"email": buyer.Email, "code": "123456"}, 400)
		call(t, "", "POST", "/api/users/password/reset", payload, 200)
		call(t, "", "POST", "/api/users/password/reset", payload, 400)
		call(t, "", "POST", "/api/users/login", gin.H{"email": buyer.Email, "password": password}, 400)
		call(t, "", "POST", "/api/users/login", gin.H{"email": buyer.Email, "password": "New-local-test-password"}, 200)
	})
}
