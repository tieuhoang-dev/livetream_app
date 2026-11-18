package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublisherPresence(t *testing.T) {
	for _, tc := range []struct {
		name, response   string
		status           int
		present, failure bool
	}{
		{"camera", `{"participants":[{"identity":"seller-owner","state":"ACTIVE","tracks":[{"source":"CAMERA","muted":true}]}]}`, 200, true, false},
		{"microphone numeric enum", `{"participants":[{"identity":"seller-owner","state":2,"tracks":[{"source":2}]}]}`, 200, true, false},
		{"viewer cannot renew", `{"participants":[{"identity":"viewer-owner","state":"ACTIVE","tracks":[{"source":"CAMERA"}]}]}`, 200, false, false},
		{"unpublished", `{"participants":[{"identity":"seller-owner","state":"ACTIVE","tracks":[]}]}`, 200, false, false},
		{"joining", `{"participants":[{"identity":"seller-owner","state":"JOINING","tracks":[{"source":"CAMERA"}]}]}`, 200, false, false},
		{"unsupported screen", `{"participants":[{"identity":"seller-owner","state":"ACTIVE","tracks":[{"source":"SCREEN_SHARE"}]}]}`, 200, false, false},
		{"missing room", `{}`, 404, false, false},
		{"outage", `{}`, 503, false, true},
		{"invalid payload", `oops`, 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/twirp/livekit.RoomService/ListParticipants" {
					t.Error("unexpected control request")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.response))
			}))
			defer server.Close()
			t.Setenv("LIVEKIT_URL", "wss://fixture.invalid")
			t.Setenv("LIVEKIT_API_URL", server.URL)
			t.Setenv("LIVEKIT_API_KEY", "test-key")
			t.Setenv("LIVEKIT_API_SECRET", "test-secret")
			present, err := livePublisherPresent(context.Background(), "live", "owner")
			if present != tc.present || (err != nil) != tc.failure {
				t.Fatalf("present=%v err=%v", present, err)
			}
		})
	}
}
