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
		p := models.Product{ID: uuid.NewString(), SellerID: owner, Name: "Test product", Price: 100, StockQuantity: quantity, ImageURL: "/test.png", Description: "Fixture"}
		if err := db.Create(&p).Error; err != nil {
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
			{"PUT", "/api/users/me"}, {"POST", "/api/users/me/avatar"}, {"POST", "/api/orders"},
			{"POST", "/api/orders/" + uuid.NewString() + "/pay"}, {"POST", "/api/orders/" + uuid.NewString() + "/cancel"},
			{"POST", "/api/orders/" + uuid.NewString() + "/refund"},
			{"POST", "/api/livestreams/" + uuid.NewString() + "/products"},
			{"DELETE", "/api/livestreams/" + uuid.NewString() + "/products/" + uuid.NewString()},
		} {
			call(t, "", endpoint[0], endpoint[1], nil, 401)
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
		var attached models.LivestreamProduct
		db.First(&attached, "livestream_id = ? AND product_code = ?", liveID, p.ID)
		if attached.ProductName != p.Name || attached.ProductImage != p.ImageURL || attached.Description != p.Description || !attached.IsPinned {
			t.Fatal("live product metadata was not copied")
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
		call(t, seller.ID, "POST", path, payload, 409)
		call(t, buyer.ID, "POST", "/api/orders", gin.H{"product_id": p.ID, "quantity": 1, "livestream_id": liveID, "payment_method": "manual"}, 409)
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
