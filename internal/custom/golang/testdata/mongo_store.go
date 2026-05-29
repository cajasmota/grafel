package store

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// User is a document shape mapped by bson tags.
type User struct {
	ID    string `bson:"_id"`
	Name  string `bson:"name"`
	Email string `bson:"email,omitempty"`
	Skip  string `bson:"-"`
}

// Order is a second document shape.
type Order struct {
	ID     string  `bson:"_id"`
	UserID string  `bson:"user_id"`
	Total  float64 `bson:"total"`
}

func usersColl(c *mongo.Client) *mongo.Collection {
	return c.Database("shop").Collection("users")
}

func ordersColl(c *mongo.Client) *mongo.Collection {
	return c.Database("shop").Collection("orders")
}

func run(ctx context.Context, c *mongo.Client) error {
	users := usersColl(c)
	orders := ordersColl(c)

	_, err := users.InsertOne(ctx, User{ID: "1", Name: "Ada"})
	if err != nil {
		return err
	}

	var u User
	_ = users.FindOne(ctx, bson.M{"name": "Ada"}).Decode(&u)

	cur, err := orders.Find(ctx, bson.M{"user_id": "1"})
	if err != nil {
		return err
	}
	_ = cur

	_, err = users.UpdateMany(ctx, bson.M{"name": "Ada"}, bson.M{"$set": bson.M{"email": "ada@x.io"}})
	if err != nil {
		return err
	}

	_, err = orders.DeleteOne(ctx, bson.M{"_id": "1"})
	if err != nil {
		return err
	}

	_, err = orders.Aggregate(ctx, mongo.Pipeline{}, options.Aggregate())
	return err
}
