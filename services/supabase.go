package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	ID           string         `json:"id"`
	Email        string         `json:"email"`
	UserMetadata map[string]any `json:"user_metadata"`
}

type InviteLink struct {
	ActionLink string   `json:"action_link"`
	URL        string   `json:"url"`
	ID         string   `json:"id"`
	Email      string   `json:"email"`
	User       AuthUser `json:"user"`
}

func (i *InviteLink) UserID() string {
	if i.ID != "" {
		return i.ID
	}
	return i.User.ID
}

func (i *InviteLink) Link() string {
	if i.ActionLink != "" {
		return i.ActionLink
	}
	return i.URL
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
	token := accessToken
	if token == "" {
		token = client.apiKey
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut {
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

func (client *SupabaseClient) GenerateInviteLink(ctx context.Context, email, redirectTo string, metadata map[string]any, result *InviteLink) error {
	return client.GenerateAuthLink(ctx, "invite", email, redirectTo, metadata, result)
}

func (client *SupabaseClient) GenerateAuthLink(ctx context.Context, linkType, email, redirectTo string, metadata map[string]any, result *InviteLink) error {
	payload := map[string]any{"type": linkType, "email": email}
	if redirectTo != "" {
		payload["redirect_to"] = redirectTo
	}
	if len(metadata) > 0 {
		payload["data"] = metadata
	}
	return client.request(ctx, http.MethodPost, "/auth/v1/admin/generate_link", client.apiKey, payload, result)
}

func (client *SupabaseClient) UpdateAdminUser(ctx context.Context, userID string, payload map[string]any, result any) error {
	return client.request(ctx, http.MethodPut, "/auth/v1/admin/users/"+userID, client.apiKey, payload, result)
}

func (client *SupabaseClient) PasswordLogin(ctx context.Context, email, password string, result any) error {
	return client.request(ctx, http.MethodPost, "/auth/v1/token?grant_type=password", "", map[string]string{
		"email":    email,
		"password": password,
	}, result)
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

func (client *SupabaseClient) Delete(ctx context.Context, path, accessToken string, result any) error {
	return client.request(ctx, http.MethodDelete, "/rest/v1"+path, accessToken, nil, result)
}

func (client *SupabaseClient) UploadStorage(ctx context.Context, bucket, objectPath, contentType string, data []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/storage/v1/object/%s/%s", client.baseURL, bucket, objectPath), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("apikey", client.apiKey)
	req.Header.Set("Authorization", "Bearer "+client.apiKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "true")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return "", &HTTPError{Status: resp.StatusCode, Message: string(respBody)}
	}

	publicURL := fmt.Sprintf("%s/storage/v1/object/public/%s/%s", client.baseURL, bucket, objectPath)
	return publicURL, nil
}


