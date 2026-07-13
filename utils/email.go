package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func SendVerificationEmail(to string, code string) error {

	apiKey := os.Getenv("BREVO_API_KEY")
	from := os.Getenv("FROM_EMAIL")

	url := "https://api.brevo.com/v3/smtp/email"

	body := map[string]interface{}{
		"sender": map[string]string{
			"email": from,
			"name":  "Your App",
		},
		"to": []map[string]string{
			{"email": to},
		},
		"subject": "Verify your account",
		"htmlContent": fmt.Sprintf(`
			<h2>Email Verification</h2>
			<p>Your verification code is:</p>
			<h1>%s</h1>
			<p>This code expires in 15 minutes.</p>
		`, code),
	}

	jsonData, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("accept", "application/json")
	req.Header.Set("api-key", apiKey)
	req.Header.Set("content-type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// ĐOẠN CODE SỬA LỖI: Bắt buộc kiểm tra Status Code của HTTP
	if resp.StatusCode >= 300 {
		// Đọc thông báo lỗi chi tiết từ Brevo
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMessage := fmt.Errorf("Lỗi API Brevo (Status %d): %s", resp.StatusCode, string(bodyBytes))
		fmt.Println("❌ Bị lỗi:", errMessage)
		return errMessage
	}

	fmt.Println("✅ Email sent to:", to)
	return nil
}
