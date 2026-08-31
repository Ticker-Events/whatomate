package firestore

import (
	"context"
	"encoding/json"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"cloud.google.com/go/firestore"
	"github.com/zerodha/logf"
	"google.golang.org/api/option"
)

// Client wraps Firestore and Firebase Auth for real-time chat sync.
type Client struct {
	firestore *firestore.Client
	auth      *auth.Client
	log       logf.Logger
}

// normalizeCredentialsJSON prepares a service-account JSON string for the Firebase SDK.
// Handles both pasted multi-line JSON and TOML/env values where PEM newlines are stored as \\n.
func normalizeCredentialsJSON(credentialsJSON string) ([]byte, error) {
	credentialsJSON = strings.TrimSpace(credentialsJSON)
	if credentialsJSON == "" {
		return nil, nil
	}

	// Same as ticker-events: escape literal newlines so json.Unmarshal can parse pasted JSON.
	normalized := strings.ReplaceAll(credentialsJSON, "\n", "\\n")

	var credCheck map[string]any
	if err := json.Unmarshal([]byte(normalized), &credCheck); err != nil {
		return nil, err
	}

	// TOML single-quoted values often keep PEM newlines as literal "\n" after JSON decode.
	if pk, ok := credCheck["private_key"].(string); ok && strings.Contains(pk, `\n`) {
		credCheck["private_key"] = strings.ReplaceAll(pk, `\n`, "\n")
		return json.Marshal(credCheck)
	}

	return []byte(normalized), nil
}

// Init creates a Firestore client from a service-account JSON string.
// Returns nil, nil when credentials are empty (Firebase disabled).
func Init(ctx context.Context, credentialsJSON string, log logf.Logger) (*Client, error) {
	credentialsJSON = strings.TrimSpace(credentialsJSON)
	if credentialsJSON == "" {
		return nil, nil
	}

	credentials, err := normalizeCredentialsJSON(credentialsJSON)
	if err != nil {
		return nil, err
	}

	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsJSON(credentials))
	if err != nil {
		return nil, err
	}

	fsClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, err
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		_ = fsClient.Close()
		return nil, err
	}

	log.Info("Firebase Firestore client initialized")

	return &Client{
		firestore: fsClient,
		auth:      authClient,
		log:       log,
	}, nil
}

// Enabled reports whether Firestore sync is active.
func (c *Client) Enabled() bool {
	return c != nil && c.firestore != nil
}

// Close releases Firestore resources.
func (c *Client) Close() error {
	if c == nil || c.firestore == nil {
		return nil
	}
	return c.firestore.Close()
}
