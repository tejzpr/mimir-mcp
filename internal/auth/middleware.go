// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"context"
	"net/http"
	"strings"
)

// ContextKey is the type for context keys
type ContextKey string

const (
	// UserIDKey is the context key for user ID
	UserIDKey ContextKey = "user_id"
	// TokenKey is the context key for the auth token
	TokenKey ContextKey = "token"
)

// Middleware provides HTTP middleware for authentication
type Middleware struct {
	tokenManager *TokenManager
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(tokenManager *TokenManager) *Middleware {
	return &Middleware{
		tokenManager: tokenManager,
	}
}

// RequireAuth is middleware that validates authentication tokens
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		token := extractToken(r)
		if token == "" {
			http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
			return
		}

		// Validate token
		authToken, err := m.tokenManager.ValidateToken(token)
		if err != nil {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// Add user ID and token to context
		ctx := context.WithValue(r.Context(), UserIDKey, authToken.UserID)
		ctx = context.WithValue(ctx, TokenKey, token)

		// Call next handler with updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth is middleware that extracts auth if present, but doesn't require it
func (m *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token != "" {
			authToken, err := m.tokenManager.ValidateToken(token)
			if err == nil {
				ctx := context.WithValue(r.Context(), UserIDKey, authToken.UserID)
				ctx = context.WithValue(ctx, TokenKey, token)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// extractToken extracts the bearer token from the Authorization header
func extractToken(r *http.Request) string {
	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Expected format: "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Check query parameter as fallback
	return r.URL.Query().Get("access_token")
}

// GetUserIDFromContext extracts the user ID from request context
func GetUserIDFromContext(ctx context.Context) (uint, bool) {
	userID, ok := ctx.Value(UserIDKey).(uint)
	return userID, ok
}

// GetTokenFromContext extracts the token from request context
func GetTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(TokenKey).(string)
	return token, ok
}

// WithUserID adds a user ID to a context (useful for testing)
func WithUserID(ctx context.Context, userID uint) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}
