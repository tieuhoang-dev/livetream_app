package services

import (
	"context"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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

func commentArchiveFixture(t *testing.T) (*gorm.DB, *mongo.Collection) {
	t.Helper()
	dsn, uri := os.Getenv("TEST_MYSQL_DSN"), os.Getenv("TEST_MONGO_URI")
	if dsn == "" || uri == "" {
		t.Skip("set TEST_MYSQL_DSN and TEST_MONGO_URI for live comment archive integration tests")
	}
	name := "comment_archive_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy:                           schema.NamingStrategy{TablePrefix: name + "_"},
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Migrator().DropTable(&models.LivestreamComment{})
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	if err := db.AutoMigrate(&models.LivestreamComment{}); err != nil {
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
		if err := collection.Database().Drop(ctx); err != nil {
			t.Error(err)
		}
		client.Disconnect(ctx)
	})
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	return db, collection
}

func insertArchiveComment(t *testing.T, db *gorm.DB) models.LivestreamComment {
	t.Helper()
	comment := models.LivestreamComment{
		LivestreamID: uuid.NewString(), UserID: uuid.NewString(),
		Username: "test-viewer", Content: "Một tin là đủ",
	}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatal(err)
	}
	return comment
}

func runTestCommentArchiver(t *testing.T, db *gorm.DB, collection *mongo.Collection) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); RunLivestreamCommentArchiver(ctx, db, collection) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("comment archiver did not stop")
		}
	})
}

func waitCommentArchived(t *testing.T, db *gorm.DB, id uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var comment models.LivestreamComment
		if err := db.First(&comment, id).Error; err != nil {
			t.Fatal(err)
		}
		if comment.MongoSavedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("one live comment was not archived without additional messages")
}

func TestLiveCommentSingleMessageArchivedByWorker(t *testing.T) {
	db, collection := commentArchiveFixture(t)
	runTestCommentArchiver(t, db, collection)
	comment := insertArchiveComment(t, db)
	waitCommentArchived(t, db, comment.ID)
	var doc struct {
		Content  string `bson:"content"`
		LiveID   string `bson:"livestream_id"`
		SenderID string `bson:"sender_id"`
		Username string `bson:"username"`
		Type     string `bson:"message_type"`
	}
	if err := collection.FindOne(context.Background(), bson.M{"comment_id": strconv.FormatUint(comment.ID, 10)}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Content != comment.Content || doc.LiveID != comment.LivestreamID || doc.SenderID != comment.UserID || doc.Username != comment.Username || doc.Type != "livestream_comment" {
		t.Fatalf("wrong archived comment: %+v", doc)
	}
}

type archiveFailureWriter struct {
	output io.Writer
	failed chan struct{}
}

func (w archiveFailureWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "Live comment Mongo archive failed") {
		select {
		case w.failed <- struct{}{}:
		default:
		}
	}
	return w.output.Write(p)
}

func TestLiveCommentMongoFailureRemainsPendingAndRetries(t *testing.T) {
	db, collection := commentArchiveFixture(t)
	ctx := context.Background()
	// Reject inserts through a real Mongo validator, without stopping shared Mongo.
	err := collection.Database().CreateCollection(ctx, collection.Name(), options.CreateCollection().SetValidator(bson.M{"reject_test_writes": bson.M{"$exists": true}}))
	if err != nil {
		t.Fatal(err)
	}
	comment := insertArchiveComment(t, db)
	failed := make(chan struct{}, 1)
	originalLog := log.Writer()
	log.SetOutput(archiveFailureWriter{output: originalLog, failed: failed})
	t.Cleanup(func() { log.SetOutput(originalLog) })
	runTestCommentArchiver(t, db, collection)
	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not attempt the Mongo write")
	}
	var pending models.LivestreamComment
	if err := db.First(&pending, comment.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.MongoSavedAt != nil {
		t.Fatal("failed Mongo write was marked archived")
	}
	if err := collection.Database().RunCommand(ctx, bson.D{{Key: "collMod", Value: collection.Name()}, {Key: "validator", Value: bson.M{}}}).Err(); err != nil {
		t.Fatal(err)
	}
	// The same worker retries automatically, without receiving another comment.
	waitCommentArchived(t, db, comment.ID)
	count, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil || count != 1 {
		t.Fatalf("recovered documents: %d, error: %v", count, err)
	}
}

func TestLiveCommentArchiveReplayDoesNotDuplicate(t *testing.T) {
	db, collection := commentArchiveFixture(t)
	comment := insertArchiveComment(t, db)
	if n, err := archiveLivestreamComments(context.Background(), db, collection); err != nil || n != 1 {
		t.Fatalf("first archive: n=%d err=%v", n, err)
	}
	// Simulate a crash after Mongo acknowledged the write but before SQL was marked.
	if err := db.Model(&models.LivestreamComment{}).Where("id = ?", comment.ID).Update("mongo_saved_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := archiveLivestreamComments(context.Background(), db, collection); err != nil || n != 1 {
		t.Fatalf("replay: n=%d err=%v", n, err)
	}
	count, err := collection.CountDocuments(context.Background(), bson.M{})
	if err != nil || count != 1 {
		t.Fatalf("duplicate documents after replay: %d, error: %v", count, err)
	}
	if n, err := archiveLivestreamComments(context.Background(), db, collection); err != nil || n != 0 {
		t.Fatalf("already archived comment read again: n=%d err=%v", n, err)
	}
}

func TestLiveCommentArchiveSmallTailDoesNotWaitForFullBatch(t *testing.T) {
	db, collection := commentArchiveFixture(t)
	for i := 0; i < commentArchiveBatchSize+1; i++ {
		insertArchiveComment(t, db)
	}
	for _, want := range []int{commentArchiveBatchSize, 1, 0} {
		if n, err := archiveLivestreamComments(context.Background(), db, collection); err != nil || n != want {
			t.Fatalf("archive batch: want=%d n=%d err=%v", want, n, err)
		}
	}
	count, err := collection.CountDocuments(context.Background(), bson.M{})
	if err != nil || count != commentArchiveBatchSize+1 {
		t.Fatalf("archived backlog: %d, error: %v", count, err)
	}
}
