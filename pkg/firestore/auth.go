package firestore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// CreateCustomToken issues a Firebase custom token with org and user claims for Firestore security rules.
func (c *Client) CreateCustomToken(ctx context.Context, userID, orgID uuid.UUID) (string, error) {
	if c == nil || c.auth == nil {
		return "", fmt.Errorf("firebase is not configured")
	}

	uid := userID.String()
	claims := map[string]any{
		"org_id":  orgID.String(),
		"user_id": userID.String(),
	}

	token, err := c.auth.CustomTokenWithClaims(ctx, uid, claims)
	if err != nil {
		return "", fmt.Errorf("create firebase custom token: %w", err)
	}
	return token, nil
}
