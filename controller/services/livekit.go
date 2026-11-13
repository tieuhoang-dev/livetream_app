package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"livesteam_app/config"
	"livesteam_app/models"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type liveKitSettings struct {
	url    string
	apiURL string
	key    string
	secret string
}

type LiveMediaSession struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func liveKitConfig() (liveKitSettings, error) {
	settings := liveKitSettings{
		url:    strings.TrimRight(os.Getenv("LIVEKIT_URL"), "/"),
		apiURL: strings.TrimRight(os.Getenv("LIVEKIT_API_URL"), "/"),
		key:    os.Getenv("LIVEKIT_API_KEY"),
		secret: os.Getenv("LIVEKIT_API_SECRET"),
	}
	if !validLiveKitOrigin(settings.url, "ws", "wss") || settings.key == "" || settings.secret == "" {
		return liveKitSettings{}, errors.New("live_media_unavailable")
	}
	if settings.apiURL == "" {
		settings.apiURL = "http" + strings.TrimPrefix(settings.url, "ws")
	}
	if !validLiveKitOrigin(settings.apiURL, "http", "https") {
		return liveKitSettings{}, errors.New("live_media_unavailable")
	}
	return settings, nil
}

func validLiveKitOrigin(value, plainScheme, secureScheme string) bool {
	origin, err := url.Parse(value)
	return err == nil && (origin.Scheme == plainScheme || origin.Scheme == secureScheme) &&
		origin.Hostname() != "" && origin.User == nil && origin.Path == "" &&
		origin.RawQuery == "" && !origin.ForceQuery && origin.Fragment == ""
}

func liveKitRoom(id string) string { return "livemart-" + id }

func (s liveKitSettings) sign(identity string, grant jwt.MapClaims) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": s.key, "nbf": now.Unix(), "exp": now.Add(90 * time.Second).Unix(), "video": grant,
	}
	if identity != "" {
		claims["sub"] = identity
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.secret))
}

func CreateLiveMediaSession(ctx context.Context, liveID, userID string, publish bool) (LiveMediaSession, error) {
	var live models.Livestream
	if err := config.DB.WithContext(ctx).Select("id", "seller_id", "status").Take(&live, "id = ?", liveID).Error; err != nil {
		return LiveMediaSession{}, lookupError(err, "livestream_not_found")
	}
	if publish && live.SellerID != userID {
		return LiveMediaSession{}, errors.New("forbidden")
	}
	if live.Status != "start" {
		return LiveMediaSession{}, errors.New("livestream_not_active")
	}
	settings, err := liveKitConfig()
	if err != nil {
		return LiveMediaSession{}, err
	}
	identity := "viewer-" + uuid.NewString()
	grant := jwt.MapClaims{
		"roomJoin": true, "room": liveKitRoom(live.ID), "canPublish": publish,
		"canSubscribe": !publish, "canPublishData": false, "canUpdateOwnMetadata": false,
	}
	if publish {
		identity = "seller-" + userID
		grant["canPublishSources"] = []string{"camera", "microphone"}
	}
	token, err := settings.sign(identity, grant)
	return LiveMediaSession{URL: settings.url, Token: token}, err
}

var liveKitHTTP = &http.Client{
	Timeout:       4 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func CloseLiveMediaRoom(liveID string) error {
	if os.Getenv("LIVEKIT_URL") == "" && os.Getenv("LIVEKIT_API_KEY") == "" && os.Getenv("LIVEKIT_API_SECRET") == "" && os.Getenv("LIVEKIT_API_URL") == "" {
		return nil
	}
	settings, err := liveKitConfig()
	if err != nil {
		return err
	}
	token, err := settings.sign("", jwt.MapClaims{"roomCreate": true})
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"room": liveKitRoom(liveID)})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, settings.apiURL+"/twirp/livekit.RoomService/DeleteRoom", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := liveKitHTTP.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("livekit DeleteRoom returned HTTP %d", response.StatusCode)
}

func EndLivestreamAndMedia(userID, liveID string) (*models.Livestream, bool, error) {
	live, err := EndLivestream(userID, liveID)
	if err != nil {
		return nil, false, err
	}
	if err := CloseLiveMediaRoom(live.ID); err != nil {
		log.Printf("Livestream %s ended, but LiveKit room cleanup failed: %v", live.ID, err)
		return live, false, nil
	}
	return live, true, nil
}
