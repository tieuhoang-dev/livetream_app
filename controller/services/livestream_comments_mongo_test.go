package services

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"livesteam_app/config"
	"livesteam_app/models"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func mongoCommentFixture(t *testing.T) (*gorm.DB, *mongo.Collection, models.Livestream) {
	t.Helper()
	dsn, uri := os.Getenv("TEST_MYSQL_DSN"), os.Getenv("TEST_MONGO_URI")
	if dsn == "" || uri == "" {
		t.Skip("set TEST_MYSQL_DSN and TEST_MONGO_URI for Mongo comment integration tests")
	}
	name := "mongo_comments_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy:                           schema.NamingStrategy{TablePrefix: name + "_"},
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tables, _ := db.Migrator().GetTables()
		for _, table := range tables {
			if strings.HasPrefix(table, name+"_") {
				db.Migrator().DropTable(table)
			}
		}
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	if err := db.AutoMigrate(&models.Livestream{}); err != nil {
		t.Fatal(err)
	}
	live := models.Livestream{ID: uuid.NewString(), SellerID: uuid.NewString(), Title: "Mongo comments", Status: "start", StartedAt: time.Now()}
	if err := db.Create(&live).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	collection := client.Database(name).Collection("chat_logs")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		collection.Database().Drop(ctx)
		client.Disconnect(ctx)
	})
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	oldDB, oldCollection := config.DB, config.ChatCollection
	config.DB, config.ChatCollection = db, collection
	t.Cleanup(func() { config.DB, config.ChatCollection = oldDB, oldCollection })
	config.EnsureMongoIndexes()
	return db, collection, live
}

func TestLiveCommentMongoIsOnlyContentStore(t *testing.T) {
	db, collection, live := mongoCommentFixture(t)
	ctx := context.Background()
	comment, err := AddLivestreamComment(ctx, live.ID, uuid.NewString(), "viewer", "  Một tin là đủ  ")
	if err != nil {
		t.Fatal(err)
	}
	var saved models.LivestreamComment
	if err := collection.FindOne(ctx, bson.M{"_id": comment.DocumentID}).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.Content != "Một tin là đủ" || saved.MessageType != "livestream_comment" || saved.LivestreamID != live.ID {
		t.Fatalf("bad Mongo document: %+v", saved)
	}
	if db.Migrator().HasTable(db.NamingStrategy.TableName("LivestreamComment")) {
		t.Fatal("new comment created SQL content storage")
	}
	if _, err := collection.UpdateOne(ctx, bson.M{"_id": comment.DocumentID}, bson.M{"$set": bson.M{"content": "Mongo is the source"}}); err != nil {
		t.Fatal(err)
	}
	items, more, err := ListLivestreamComments(ctx, live.ID, 0)
	if err != nil || more || len(items) != 1 || items[0].Content != "Mongo is the source" {
		t.Fatalf("history not read directly from Mongo: %+v %v %v", items, more, err)
	}
}

func TestLiveCommentMongoPaginationAndRoomScope(t *testing.T) {
	db, collection, live := mongoCommentFixture(t)
	ctx := context.Background()
	for i := 0; i < commentPageSize+2; i++ {
		if _, err := AddLivestreamComment(ctx, live.ID, uuid.NewString(), "viewer", "Hi"); err != nil {
			t.Fatal(err)
		}
	}
	latest, more, err := ListLivestreamComments(ctx, live.ID, 0)
	if err != nil || more || len(latest) != 50 || latest[0].ID != 3 || latest[49].ID != 52 {
		t.Fatalf("latest page: len=%d more=%v err=%v", len(latest), more, err)
	}
	page, more, err := ListLivestreamComments(ctx, live.ID, 1)
	if err != nil || !more || len(page) != 50 || page[0].ID != 2 || page[49].ID != 51 {
		t.Fatal("incremental Mongo page incorrect", err)
	}
	tail, more, err := ListLivestreamComments(ctx, live.ID, 51)
	if err != nil || more || len(tail) != 1 || tail[0].ID != 52 {
		t.Fatal("single remaining message was not returned", err)
	}
	other := models.Livestream{ID: uuid.NewString(), SellerID: live.SellerID, Status: "start", StartedAt: time.Now()}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := AddLivestreamComment(ctx, other.ID, uuid.NewString(), "other", "Other room"); err != nil {
		t.Fatal(err)
	}
	collection.InsertOne(ctx, bson.M{"_id": uuid.NewString(), "livestream_id": live.ID, "message_type": "text", "sequence": int64(999999), "content": "private chat"})
	tail, _, err = ListLivestreamComments(ctx, live.ID, 51)
	if err != nil || len(tail) != 1 {
		t.Fatal("other room/private messages leaked into comments", err)
	}
}

func TestLiveCommentMongoFailureDoesNotFallBackToSQL(t *testing.T) {
	db, collection, live := mongoCommentFixture(t)
	ctx := context.Background()
	if err := collection.Database().RunCommand(ctx, bson.D{{Key: "collMod", Value: collection.Name()}, {Key: "validator", Value: bson.M{"reject_writes": bson.M{"$exists": true}}}}).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLivestreamComment(ctx, live.ID, uuid.NewString(), "viewer", "Must fail"); err == nil {
		t.Fatal("Mongo rejection acknowledged as a sent comment")
	}
	if db.Migrator().HasTable(db.NamingStrategy.TableName("LivestreamComment")) {
		t.Fatal("Mongo failure created a SQL fallback")
	}
	if n, err := collection.CountDocuments(ctx, bson.M{}); err != nil || n != 0 {
		t.Fatal("rejected comment persisted", n, err)
	}
	if err := collection.Database().RunCommand(ctx, bson.D{{Key: "collMod", Value: collection.Name()}, {Key: "validator", Value: bson.M{}}}).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLivestreamComment(ctx, live.ID, uuid.NewString(), "viewer", "Recovered"); err != nil {
		t.Fatal(err)
	}
	items, _, err := ListLivestreamComments(ctx, live.ID, 0)
	if err != nil || len(items) != 1 || items[0].Content != "Recovered" {
		t.Fatal("counter gap broke history after recovery", err)
	}
}

func TestLiveCommentConcurrentMongoSequences(t *testing.T) {
	_, collection, live := mongoCommentFixture(t)
	var wait sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := AddLivestreamComment(context.Background(), live.ID, uuid.NewString(), "viewer", "Concurrent")
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, err := collection.CountDocuments(context.Background(), bson.M{}); err != nil || n != 12 {
		t.Fatal("concurrent comment lost/duplicated", n, err)
	}
	items, _, err := ListLivestreamComments(context.Background(), live.ID, 0)
	if err != nil || len(items) != 12 {
		t.Fatal("concurrent history", err)
	}
	for i, item := range items {
		if item.ID != uint64(i+1) {
			t.Fatal("non-monotonic comment cursor")
		}
	}
}

func TestLegacyCommentsImportToMongoOnce(t *testing.T) {
	db, collection, live := mongoCommentFixture(t)
	ctx := context.Background()
	table := db.NamingStrategy.TableName("LivestreamComment")
	if err := MigrateLegacyLivestreamComments(ctx, db, collection); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable(table) {
		t.Fatal("migration created a legacy SQL table")
	}
	if err := db.Table(table).AutoMigrate(&legacyLivestreamComment{}); err != nil {
		t.Fatal(err)
	}
	old := legacyLivestreamComment{ID: 500, LivestreamID: live.ID, UserID: uuid.NewString(), Username: "old-viewer", Content: "Old history", CreatedAt: time.Now().Add(-24 * time.Hour).Truncate(time.Millisecond)}
	if err := db.Table(table).Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	// Also support documents produced by the superseded SQL archive approach.
	collection.InsertOne(ctx, bson.M{"_id": liveCommentDocumentID(live.ID, old.ID), "comment_id": "500", "content": "Old archive", "message_type": "livestream_comment"})
	if err := MigrateLegacyLivestreamComments(ctx, db, collection); err != nil {
		t.Fatal(err)
	}
	items, _, err := ListLivestreamComments(ctx, live.ID, 0)
	if err != nil || len(items) != 1 || items[0].ID != 500 || items[0].Content != old.Content || items[0].CreatedAt.UnixMilli() != old.CreatedAt.UnixMilli() {
		t.Fatal("legacy history not preserved", items, err)
	}
	comment, err := AddLivestreamComment(ctx, live.ID, uuid.NewString(), "new-viewer", "New Mongo message")
	if err != nil || comment.ID != 501 {
		t.Fatal("Mongo counter did not continue after old SQL IDs", err)
	}
	if _, err := collection.UpdateOne(ctx, bson.M{"_id": items[0].DocumentID}, bson.M{"$set": bson.M{"content": "Mongo is authoritative"}}); err != nil {
		t.Fatal(err)
	}
	if err := MigrateLegacyLivestreamComments(ctx, db, collection); err != nil {
		t.Fatal(err)
	}
	items, _, err = ListLivestreamComments(ctx, live.ID, 0)
	if err != nil || items[0].Content != "Mongo is authoritative" {
		t.Fatal("completed migration reread SQL as the source")
	}
	var rows int64
	if err := db.Table(table).Count(&rows).Error; err != nil || rows != 1 {
		t.Fatal("new comment was written into legacy SQL history", rows, err)
	}
}

func TestLegacyCommentsMigrationRetriesMongoFailure(t *testing.T) {
	db, collection, live := mongoCommentFixture(t)
	ctx := context.Background()
	table := db.NamingStrategy.TableName("LivestreamComment")
	if err := db.Table(table).AutoMigrate(&legacyLivestreamComment{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table(table).Create(&legacyLivestreamComment{ID: 8, LivestreamID: live.ID, Username: "viewer", Content: "Legacy", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := collection.Database().RunCommand(ctx, bson.D{{Key: "collMod", Value: collection.Name()}, {Key: "validator", Value: bson.M{"reject": bson.M{"$exists": true}}}}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := MigrateLegacyLivestreamComments(ctx, db, collection); err == nil {
		t.Fatal("failed import marked complete")
	}
	if err := collection.Database().RunCommand(ctx, bson.D{{Key: "collMod", Value: collection.Name()}, {Key: "validator", Value: bson.M{}}}).Err(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := MigrateLegacyLivestreamComments(ctx, db, collection); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := collection.CountDocuments(ctx, bson.M{}); err != nil || n != 1 {
		t.Fatal("import replay duplicated/lost history", n, err)
	}
}
