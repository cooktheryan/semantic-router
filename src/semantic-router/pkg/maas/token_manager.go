package maas

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// TokenManager manages MaaS authentication tokens with automatic refresh
type TokenManager struct {
	apiURL                  string
	serviceAccountTokenPath string
	tokenExpiration         time.Duration

	// Token cache
	mu                sync.RWMutex
	cachedToken       string
	tokenExpiresAt    time.Time

	// HTTP client for token requests
	httpClient *http.Client
}

// TokenResponse represents the response from MaaS token API
type TokenResponse struct {
	Token      string `json:"token"`
	Expiration string `json:"expiration"`
	ExpiresAt  int64  `json:"expiresAt"`
}

// TokenRequest represents the request body for MaaS token API
type TokenRequest struct {
	Expiration string `json:"expiration"`
}

// NewTokenManager creates a new MaaS token manager
func NewTokenManager(apiURL, serviceAccountTokenPath, tokenExpiration string) (*TokenManager, error) {
	// Parse token expiration duration
	duration, err := time.ParseDuration(tokenExpiration)
	if err != nil {
		return nil, fmt.Errorf("invalid token expiration duration: %w", err)
	}

	// Create HTTP client with TLS config (skip verification for self-signed certs)
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Required for self-signed certificates in OpenShift
			},
		},
	}

	tm := &TokenManager{
		apiURL:                  apiURL,
		serviceAccountTokenPath: serviceAccountTokenPath,
		tokenExpiration:         duration,
		httpClient:              httpClient,
	}

	return tm, nil
}

// GetToken returns a valid MaaS token, acquiring or refreshing as needed
func (tm *TokenManager) GetToken() (string, error) {
	tm.mu.RLock()

	// Check if we have a valid cached token
	if tm.cachedToken != "" && time.Now().Before(tm.tokenExpiresAt) {
		token := tm.cachedToken
		tm.mu.RUnlock()
		logging.Debugf("Using cached MaaS token (expires at %s)", tm.tokenExpiresAt.Format(time.RFC3339))
		return token, nil
	}

	tm.mu.RUnlock()

	// Need to acquire or refresh token
	return tm.refreshToken()
}

// refreshToken acquires a new token from the MaaS API
func (tm *TokenManager) refreshToken() (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double-check: another goroutine might have refreshed while we were waiting
	if tm.cachedToken != "" && time.Now().Before(tm.tokenExpiresAt) {
		logging.Debugf("Token was refreshed by another goroutine, using cached token")
		return tm.cachedToken, nil
	}

	logging.Infof("Acquiring new MaaS token (expiration: %s)", tm.tokenExpiration)

	// Read ServiceAccount token from mounted file
	saToken, err := tm.readServiceAccountToken()
	if err != nil {
		return "", fmt.Errorf("failed to read ServiceAccount token: %w", err)
	}

	// Prepare token request
	tokenReq := TokenRequest{
		Expiration: tm.tokenExpiration.String(),
	}

	reqBody, err := json.Marshal(tokenReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token request: %w", err)
	}

	// Construct token endpoint URL
	tokenEndpoint := fmt.Sprintf("%s/maas-api/v1/tokens", tm.apiURL)

	// Create HTTP request
	req, err := http.NewRequest("POST", tokenEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", saToken))

	// Send request
	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send token request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	// Calculate token expiration time (refresh at 80% of TTL to avoid edge cases)
	refreshMargin := time.Duration(float64(tm.tokenExpiration) * 0.8)
	expiresAt := time.Now().Add(refreshMargin)

	// Cache the token
	tm.cachedToken = tokenResp.Token
	tm.tokenExpiresAt = expiresAt

	logging.Infof("Successfully acquired MaaS token (expires at %s, will refresh at %s)",
		time.Unix(tokenResp.ExpiresAt, 0).Format(time.RFC3339),
		expiresAt.Format(time.RFC3339))
	logging.Debugf("MaaS token (first 50 chars): %s...", tokenResp.Token[:min(50, len(tokenResp.Token))])

	return tokenResp.Token, nil
}

// readServiceAccountToken reads the Kubernetes ServiceAccount token from the mounted file
func (tm *TokenManager) readServiceAccountToken() (string, error) {
	data, err := os.ReadFile(tm.serviceAccountTokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", tm.serviceAccountTokenPath, err)
	}

	token := string(data)
	if token == "" {
		return "", fmt.Errorf("ServiceAccount token is empty")
	}

	logging.Debugf("Read ServiceAccount token from %s (length: %d bytes)", tm.serviceAccountTokenPath, len(token))
	return token, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
