package config

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ChatCollection *mongo.Collection

func ConnectMongoDB(ctx context.Context) error {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		return errors.New("MONGO_URI is not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return err
	}
	if err := client.Ping(ctx, nil); err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		client.Disconnect(cleanup)
		return err
	}
	database := os.Getenv("MONGO_DATABASE")
	if database == "" {
		database = "livestream_app"
	}
	ChatCollection = client.Database(database).Collection("chat_logs")
	log.Println("MongoDB connected")
	return nil
}

func EnsureMongoIndexes(ctx context.Context) error {
	if ChatCollection == nil {
		return errors.New("MongoDB ChatCollection is not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := ChatCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "livestream_id", Value: 1}, {Key: "created_at", Value: 1}}},
		{Keys: bson.D{{Key: "livestream_id", Value: 1}, {Key: "sequence", Value: 1}}, Options: options.Index().SetName("live_comment_sequence").SetUnique(true).SetPartialFilterExpression(bson.M{"message_type": "livestream_comment", "sequence": bson.M{"$exists": true}})},
	})
	return err
}
