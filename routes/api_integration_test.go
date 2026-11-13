package routes_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
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
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
	t.Chdir(t.TempDir())
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	models.MigrateAll(db)
	oldDB, oldControllerDB := config.DB, controllers.DB
	config.DB = db
	controllers.SetDatabase(db)
	t.Cleanup(func() {
		config.DB = oldDB
		controllers.SetDatabase(oldControllerDB)
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
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
	seller, buyer := newUser(), newUser()
	ids := []string{seller.ID, buyer.ID}
	t.Cleanup(func() {
		db.Exec("DELETE FROM payments WHERE order_id IN (SELECT id FROM orders WHERE user_id IN ?)", ids)
		db.Exec("DELETE FROM order_items WHERE order_id IN (SELECT id FROM orders WHERE user_id IN ?)", ids)
		db.Where("user_id IN ?", ids).Delete(&models.Order{})
		db.Exec("DELETE FROM livestream_products WHERE livestream_id IN (SELECT id FROM livestreams WHERE seller_id IN ?)", ids)
		db.Where("seller_id IN ?", ids).Delete(&models.Livestream{})
		db.Where("seller_id IN ?", ids).Delete(&models.Product{})
		db.Where("id IN ?", ids).Delete(&models.User{})
	})
	tokens := map[string]string{}
	for _, u := range []models.User{seller, buyer} {
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
		out := call(t, owner, "POST", "/api/products", gin.H{"name": "Test product", "price": 100, "stock_quantity": quantity, "image_url": "/uploads/test.png", "description": "Fixture"}, 201)
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
			{"POST", "/api/products"}, {"PATCH", "/api/products/" + uuid.NewString()},
			{"DELETE", "/api/products/" + uuid.NewString()}, {"POST", "/api/products/" + uuid.NewString() + "/stock"},
			{"PUT", "/api/users/me"}, {"POST", "/api/users/me/avatar"}, {"POST", "/api/orders"},
			{"GET", "/api/orders"}, {"GET", "/api/orders/" + uuid.NewString()},
			{"GET", "/api/sales/offline"}, {"GET", "/api/sales/offline/" + uuid.NewString()},
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
		id := order(t, p, nil, 2)
		call(t, seller.ID, "DELETE", "/api/products/"+p.ID, nil, 409)
		call(t, seller.ID, "PATCH", "/api/products/"+p.ID, gin.H{"price": 120}, 200)
		if stock(t, p.ID) != 3 {
			t.Fatal("stock was not deducted")
		}
		for _, action := range []string{"pay", "cancel", "refund"} {
			call(t, seller.ID, "POST", "/api/orders/"+id+"/"+action, gin.H{"payment_method": "manual", "refund_reason": "test"}, 404)
		}
		call(t, buyer.ID, "POST", "/api/orders/"+id+"/pay", gin.H{"payment_method": "manual"}, 200)
		call(t, buyer.ID, "POST", "/api/orders/"+id+"/pay", gin.H{"payment_method": "manual"}, 409)
		call(t, buyer.ID, "POST", "/api/orders/"+id+"/cancel", nil, 409)
		call(t, buyer.ID, "POST", "/api/orders/"+id+"/refund", gin.H{"refund_reason": "Test refund"}, 200)
		call(t, buyer.ID, "POST", "/api/orders/"+id+"/refund", gin.H{"refund_reason": "Retry"}, 409)
		var payment models.Payment
		db.First(&payment, "order_id = ?", id)
		if stock(t, p.ID) != 5 || payment.Status != "refunded" || payment.Amount != 200 {
			t.Fatal("refund did not restore stock once and synchronize payment")
		}
		call(t, seller.ID, "DELETE", "/api/products/"+p.ID, nil, 409)
		cancelID := order(t, p, nil, 1)
		call(t, buyer.ID, "POST", "/api/orders/"+cancelID+"/cancel", nil, 200)
		call(t, buyer.ID, "POST", "/api/orders/"+cancelID+"/cancel", nil, 409)
		call(t, buyer.ID, "POST", "/api/orders/"+cancelID+"/pay", gin.H{"payment_method": "manual"}, 409)
		if stock(t, p.ID) != 5 {
			t.Fatal("repeated cancellation changed stock")
		}
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

	t.Run("concurrent orders cannot oversell", func(t *testing.T) {
		p := product(t, seller.ID, 1)
		codes := make(chan int, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				payload := fmt.Sprintf(`{"product_id":%q,"quantity":1,"payment_method":"manual"}`, p.ID)
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
		call(t, buyer.ID, "POST", "/api/orders/"+paidID+"/pay", gin.H{"payment_method": "manual"}, 200)
		call(t, buyer.ID, "POST", "/api/orders/"+paidID+"/refund", gin.H{"refund_reason": "Test"}, 200)
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
		call(t, buyer.ID, "POST", "/api/orders/"+paidID+"/pay", gin.H{"payment_method": "manual"}, 200)
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
		call(t, buyer.ID, "POST", "/api/orders/"+paidID+"/refund", gin.H{"refund_reason": "Test refund"}, 200)
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
