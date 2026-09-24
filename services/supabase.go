package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type SupabaseClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewSupabaseClient(baseURL, apiKey string) *SupabaseClient {
	return &SupabaseClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

func NewSupabaseAdminClient(baseURL, serviceRoleKey string) *SupabaseClient {
	return NewSupabaseClient(baseURL, serviceRoleKey)
}

type HTTPError struct {
	Status  int
	Message string
}

type AuthUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (err *HTTPError) Error() string {
	return err.Message
}

func (client *SupabaseClient) request(ctx context.Context, method, path, accessToken string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", client.apiKey)
	req.Header.Set("Accept", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodPatch {
		req.Header.Set("Prefer", "return=representation")
	}

	response, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return &HTTPError{Status: response.StatusCode, Message: message}
	}
	if result == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return fmt.Errorf("decode Supabase response: %w", err)
	}
	return nil
}

func (client *SupabaseClient) AuthUser(ctx context.Context, accessToken string, result any) error {
	return client.request(ctx, http.MethodGet, "/auth/v1/user", accessToken, nil, result)
}

func (client *SupabaseClient) InviteUser(ctx context.Context, email, redirectTo string, result any) error {
	path := "/auth/v1/invite"
	if redirectTo != "" {
		path += "?redirect_to=" + url.QueryEscape(redirectTo)
	}
	return client.request(ctx, http.MethodPost, path, client.apiKey, map[string]any{"email": email}, result)
}

func (client *SupabaseClient) Get(ctx context.Context, path, accessToken string, result any) error {
	return client.request(ctx, http.MethodGet, "/rest/v1"+path, accessToken, nil, result)
}

func (client *SupabaseClient) Post(ctx context.Context, path, accessToken string, payload, result any) error {
	return client.request(ctx, http.MethodPost, "/rest/v1"+path, accessToken, payload, result)
}

func (client *SupabaseClient) Patch(ctx context.Context, path, accessToken string, payload, result any) error {
	return client.request(ctx, http.MethodPatch, "/rest/v1"+path, accessToken, payload, result)
}
