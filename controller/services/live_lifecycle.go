package services

import (
	"context"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"livesteam_app/config"
	"livesteam_app/models"
)

const LiveSignalGrace = 90 * time.Second
const liveSignalCheckInterval = 15 * time.Second

// RunLivestreamLifecycle observes the media server, not the chat connection.
// A browser with an open WebSocket but no published media cannot keep a live up.
func RunLivestreamLifecycle(ctx context.Context) {
	tick := time.NewTicker(liveSignalCheckInterval)
	defer tick.Stop()
	for {
		if err := ReconcileLivestreams(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("Live lifecycle: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// ReconcileLivestreams also handles persisted sessions after a backend restart.
// Legacy rows receive one initial grace period, saved in SQL rather than reset
// on each process restart. Media API calls never hold database locks.
func ReconcileLivestreams(ctx context.Context, now time.Time) error {
	deadline := now.Add(LiveSignalGrace)
	db := config.DB.WithContext(ctx)
	if err := db.Model(&models.Livestream{}).Where("status = ? AND signal_expires_at IS NULL", "start").Update("signal_expires_at", deadline).Error; err != nil {
		return err
	}
	var lives []models.Livestream
	if err := db.Select("id", "seller_id", "signal_expires_at").Where("status = ?", "start").Find(&lives).Error; err != nil {
		return err
	}
	jobs := make(chan models.Livestream)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for live := range jobs {
				present, err := livePublisherPresent(ctx, live.ID, live.SellerID)
				if ctx.Err() != nil {
					continue
				}
				if err == nil && present {
					if err := db.Model(&models.Livestream{}).Where("id = ? AND status = ?", live.ID, "start").Updates(map[string]any{"last_signal_at": now, "signal_expires_at": deadline}).Error; err != nil {
						log.Printf("Live %s signal renewal: %v", live.ID, err)
					}
					continue
				}
				if live.SignalExpiresAt != nil && !live.SignalExpiresAt.After(now) {
					ended, endErr := endExpiredLivestream(ctx, live.ID, now)
					if endErr != nil {
						log.Printf("Live %s expiration: %v", live.ID, endErr)
						continue
					}
					if ended {
						log.Printf("Live %s ended after publisher signal timeout", live.ID)
						if err := CloseLiveMediaRoom(live.ID); err != nil {
							log.Printf("Live %s media cleanup: %v", live.ID, err)
						}
					}
				}
			}
		}()
	}
	for _, live := range lives {
		select {
		case jobs <- live:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return ctx.Err()
}

func endExpiredLivestream(ctx context.Context, id string, now time.Time) (bool, error) {
	ended := false
	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var live models.Livestream
		// Use the same row lock and inventory path as an explicit live.end.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&live, "id = ?", id).Error; err != nil {
			return err
		}
		if live.ID == "" || live.Status != "start" || live.SignalExpiresAt == nil || live.SignalExpiresAt.After(now) {
			return nil
		}
		if err := finishLivestream(tx, &live); err != nil {
			return err
		}
		ended = true
		return nil
	})
	if err == nil && ended {
		liveChanged(id)
		shippingChanged(id)
	}
	return ended, err
}

func EndOwnedLivestreams(userID string) ([]string, bool, error) {
	var ids []string
	if err := config.DB.Model(&models.Livestream{}).Where("seller_id = ? AND status = ?", userID, "start").Order("id").Pluck("id", &ids).Error; err != nil {
		return nil, false, err
	}
	// Finish SQL for every live before waiting on media cleanup.
	for _, id := range ids {
		if _, err := EndLivestream(userID, id); err != nil {
			return nil, false, err
		}
	}
	closed := true
	for _, id := range ids {
		if err := CloseLiveMediaRoom(id); err != nil {
			closed = false
			log.Printf("Live %s logout media cleanup: %v", id, err)
		}
	}
	return ids, closed, nil
}
