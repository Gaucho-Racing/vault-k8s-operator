package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	HTTPClient *http.Client
}

type secretRequest struct {
	Token   string            `json:"token"`
	Secrets map[string]string `json:"secrets"`
}

type secretResponse struct {
	Secrets map[string]string `json:"secrets"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewClient() *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) PullSecrets(ctx context.Context, vaultURL string, token string, secrets map[string]string) (map[string]string, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}

	body, err := json.Marshal(secretRequest{Token: token, Secrets: secrets})
	if err != nil {
		return map[string]string{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL(vaultURL)+"/integrations/kubernetes/secrets", bytes.NewReader(body))
	if err != nil {
		return map[string]string{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "vault-k8s-operator")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return map[string]string{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errorBody errorResponse
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&errorBody)
		if errorBody.Error != "" {
			return map[string]string{}, fmt.Errorf("vault returned HTTP %d: %s", resp.StatusCode, errorBody.Error)
		}
		return map[string]string{}, fmt.Errorf("vault returned HTTP %d", resp.StatusCode)
	}

	var result secretResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return map[string]string{}, err
	}
	if result.Secrets == nil {
		return map[string]string{}, nil
	}
	return result.Secrets, nil
}

func apiURL(vaultURL string) string {
	base := strings.TrimRight(strings.TrimSpace(vaultURL), "/")
	return strings.TrimSuffix(base, "/api") + "/api"
}
