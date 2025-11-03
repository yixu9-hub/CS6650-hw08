package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDB client wrapper
type DynamoDBClient struct {
	client    *dynamodb.Client
	tableName string
}

// Cart item structure for embedded JSON
type CartItem struct {
	ProductID int `json:"product_id" dynamodbav:"product_id"`
	Quantity  int `json:"quantity" dynamodbav:"quantity"`
}

// DynamoDB cart record with embedded items (single-table design)
type DynamoCart struct {
	CartID     string     `dynamodbav:"cart_id"`
	CustomerID int        `dynamodbav:"customer_id"`
	Items      []CartItem `dynamodbav:"items"`
	CreatedAt  string     `dynamodbav:"created_at"`
	UpdatedAt  string     `dynamodbav:"updated_at"`
}

// Initialize DynamoDB client from environment variables
func initDynamoDB() (*DynamoDBClient, error) {
	tableName := os.Getenv("DYNAMODB_TABLE_NAME")
	if tableName == "" {
		return nil, fmt.Errorf("missing DYNAMODB_TABLE_NAME environment variable")
	}

	// Load AWS SDK configuration from environment (uses IAM role credentials)
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(getenv("AWS_REGION", "us-west-2")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &DynamoDBClient{
		client:    dynamodb.NewFromConfig(cfg),
		tableName: tableName,
	}, nil
}

// Create a new shopping cart in DynamoDB
func (ddb *DynamoDBClient) CreateCart(ctx context.Context, customerID int) (string, error) {
	// Generate cart_id using timestamp to make it sortable and quasi-unique
	// Format: Unix nano timestamp as string for DynamoDB hash key
	cartID := fmt.Sprintf("%d", time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339)

	cart := DynamoCart{
		CartID:     cartID,
		CustomerID: customerID,
		Items:      []CartItem{}, // Empty items array
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	item, err := attributevalue.MarshalMap(cart)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cart: %w", err)
	}

	_, err = ddb.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(ddb.tableName),
		Item:      item,
	})
	if err != nil {
		return "", fmt.Errorf("failed to put item: %w", err)
	}

	return cartID, nil
}

// Get a shopping cart by ID
func (ddb *DynamoDBClient) GetCart(ctx context.Context, cartID string) (*DynamoCart, error) {
	result, err := ddb.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(ddb.tableName),
		Key: map[string]types.AttributeValue{
			"cart_id": &types.AttributeValueMemberS{Value: cartID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}

	if result.Item == nil {
		return nil, errors.New("cart not found")
	}

	var cart DynamoCart
	err = attributevalue.UnmarshalMap(result.Item, &cart)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal cart: %w", err)
	}

	return &cart, nil
}

// Add, update, or remove an item from a cart (quantity=0 removes the item)
func (ddb *DynamoDBClient) UpdateCartItems(ctx context.Context, cartID string, productID, quantity int) error {
	// First, get the current cart to modify items
	cart, err := ddb.GetCart(ctx, cartID)
	if err != nil {
		return err
	}

	// Find and update the item in the embedded items list
	found := false
	newItems := []CartItem{}
	
	for _, item := range cart.Items {
		if item.ProductID == productID {
			found = true
			if quantity > 0 {
				// Update quantity
				newItems = append(newItems, CartItem{ProductID: productID, Quantity: quantity})
			}
			// If quantity == 0, skip adding (remove item)
		} else {
			newItems = append(newItems, item)
		}
	}

	// If not found and quantity > 0, add new item
	if !found && quantity > 0 {
		newItems = append(newItems, CartItem{ProductID: productID, Quantity: quantity})
	}

	// Update the cart with new items list
	cart.Items = newItems
	cart.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	item, err := attributevalue.MarshalMap(cart)
	if err != nil {
		return fmt.Errorf("failed to marshal updated cart: %w", err)
	}

	_, err = ddb.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(ddb.tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("failed to update cart: %w", err)
	}

	return nil
}

// Helper function to convert DynamoCart to the API response format
func dynamoCartToResponse(cart *DynamoCart) map[string]interface{} {
	items := make([]map[string]interface{}, len(cart.Items))
	for i, item := range cart.Items {
		items[i] = map[string]interface{}{
			"product_id": item.ProductID,
			"quantity":   item.Quantity,
		}
	}

	createdAt, _ := time.Parse(time.RFC3339, cart.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, cart.UpdatedAt)

	return map[string]interface{}{
		"cart": map[string]interface{}{
			"cart_id":     cart.CartID,  // String for DynamoDB
			"customer_id": cart.CustomerID,
			"status":      "active",
			"created_at":  createdAt,
			"updated_at":  updatedAt,
		},
		"items": items,
	}
}

// DynamoDB-backed handlers

// Create shopping cart handler for DynamoDB
func createShoppingCartHandlerDynamo(ddb *DynamoDBClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		
		var req createCartReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "INVALID_INPUT", "Invalid JSON")
			return
		}
		if req.CustomerID < 1 {
			writeErr(w, 400, "INVALID_INPUT", "customer_id must be >= 1")
			return
		}

		cartID, err := ddb.CreateCart(context.Background(), req.CustomerID)
		if err != nil {
			writeErr(w, 500, "DYNAMODB_ERROR", err.Error())
			return
		}

		// Return cart_id as integer for compatibility with MySQL version
		// Parse the numeric cart_id back to int64
		cartIDInt, _ := strconv.ParseInt(cartID, 10, 64)
		writeJSON(w, 201, createCartResp{ShoppingCartID: int(cartIDInt)})
	}
}

// Add items to cart handler for DynamoDB
func addItemsToCartHandlerDynamo(ddb *DynamoDBClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		// Parse cart_id from URL path
		after := strings.TrimPrefix(r.URL.Path, "/shopping-carts/")
		parts := strings.Split(after, "/")
		if len(parts) < 2 || parts[1] != "items" {
			http.NotFound(w, r)
			return
		}

		cartID := parts[0]
		if cartID == "" {
			writeErr(w, 400, "INVALID_INPUT", "cart_id is required")
			return
		}

		var req addItemsReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "INVALID_INPUT", "Invalid JSON")
			return
		}
		if req.ProductID < 1 || req.Quantity < 0 {
			writeErr(w, 400, "INVALID_INPUT", "product_id must be >=1 and quantity >=0")
			return
		}

		err := ddb.UpdateCartItems(context.Background(), cartID, req.ProductID, req.Quantity)
		if err != nil {
			if err.Error() == "cart not found" {
				writeErr(w, 404, "NOT_FOUND", "shopping cart not found")
				return
			}
			writeErr(w, 500, "DYNAMODB_ERROR", err.Error())
			return
		}

		w.WriteHeader(204)
	}
}

// Get shopping cart handler for DynamoDB
func getShoppingCartHandlerDynamo(ddb *DynamoDBClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		// Parse cart_id from URL path
		after := strings.TrimPrefix(r.URL.Path, "/shopping-carts/")
		cartID := after
		if cartID == "" {
			writeErr(w, 400, "INVALID_INPUT", "cart_id is required")
			return
		}

		cart, err := ddb.GetCart(context.Background(), cartID)
		if err != nil {
			if err.Error() == "cart not found" {
				writeErr(w, 404, "NOT_FOUND", "shopping cart not found")
				return
			}
			writeErr(w, 500, "DYNAMODB_ERROR", err.Error())
			return
		}

		resp := dynamoCartToResponse(cart)
		writeJSON(w, 200, resp)
	}
}
