package teststudio

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedRunner(t *testing.T) {
	handler := Handler()
	for _, path := range []string{"/", "/app.js", "/runner.js", "/sandbox.worker.js", "/data/collection.json"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if response.Code != 200 {
				t.Fatalf("%s: HTTP %d", path, response.Code)
			}
			if path == "/data/collection.json" {
				var collection struct {
					Item []json.RawMessage `json:"item"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &collection); err != nil || len(collection.Item) != 7 {
					t.Fatal("embedded collection is missing or invalid")
				}
			}
			if path == "/" && !strings.Contains(response.Body.String(), "Livestream Test Studio") {
				t.Fatal("wrong UI served")
			}
		})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/../.env", nil))
	if response.Code == 200 {
		t.Fatal("non-embedded file must not be served")
	}
}
