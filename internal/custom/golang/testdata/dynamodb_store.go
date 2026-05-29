package store

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// User is a DynamoDB item shape mapped by dynamodbav tags.
type User struct {
	ID    string `dynamodbav:"id"`
	Name  string `dynamodbav:"name"`
	Email string `dynamodbav:"email,omitempty"`
	Skip  string `dynamodbav:"-"`
}

// Order is a second item shape.
type Order struct {
	ID     string  `dynamodbav:"id"`
	UserID string  `dynamodbav:"user_id"`
	Total  float64 `dynamodbav:"total"`
}

func run(ctx context.Context, c *dynamodb.Client) error {
	_, err := c.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String("users"),
	})
	if err != nil {
		return err
	}

	_, err = c.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String("users"),
	})
	if err != nil {
		return err
	}

	_, err = c.Query(ctx, &dynamodb.QueryInput{
		TableName: aws.String("orders"),
	})
	if err != nil {
		return err
	}

	_, err = c.UpdateItem(ctx, &dynamodb.UpdateItemInput{TableName: aws.String("users")})
	if err != nil {
		return err
	}

	_, err = c.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String("orders")})
	return err
}
