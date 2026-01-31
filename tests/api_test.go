package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestConfig holds test configuration
type TestConfig struct {
	BaseURL string
}

var testConfig = TestConfig{
	BaseURL: getEnvOrDefault("TEST_BASE_URL", "http://localhost:8080"),
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// APIClient is a test HTTP client
type APIClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// NewAPIClient creates a new API client
func NewAPIClient() *APIClient {
	return &APIClient{
		baseURL: testConfig.BaseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SetToken sets the auth token
func (c *APIClient) SetToken(token string) {
	c.token = token
}

// APIResponse represents a standard API response
type APIResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Request makes an HTTP request
func (c *APIClient) Request(method, path string, body interface{}) (*APIResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w, body: %s", err, string(respBody))
	}

	return &apiResp, nil
}

// GET makes a GET request
func (c *APIClient) GET(path string) (*APIResponse, error) {
	return c.Request(http.MethodGet, path, nil)
}

// POST makes a POST request
func (c *APIClient) POST(path string, body interface{}) (*APIResponse, error) {
	return c.Request(http.MethodPost, path, body)
}

// PUT makes a PUT request
func (c *APIClient) PUT(path string, body interface{}) (*APIResponse, error) {
	return c.Request(http.MethodPut, path, body)
}

// ParseData parses response data into target struct
func (r *APIResponse) ParseData(v interface{}) error {
	if r.Data == nil {
		return nil
	}
	return json.Unmarshal(r.Data, v)
}

// IsSuccess checks if response is successful
func (r *APIResponse) IsSuccess() bool {
	return r.Code == 0
}

// AssertSuccess asserts response is successful
func AssertSuccess(t *testing.T, resp *APIResponse, msgAndArgs ...interface{}) {
	t.Helper()
	if !resp.IsSuccess() {
		msg := fmt.Sprintf("expected success, got code=%d, msg=%s", resp.Code, resp.Msg)
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf("%s: %v", msg, msgAndArgs)
		}
		t.Fatal(msg)
	}
}

// AssertError asserts response has specific error code
func AssertError(t *testing.T, resp *APIResponse, expectedCode int, msgAndArgs ...interface{}) {
	t.Helper()
	if resp.Code != expectedCode {
		msg := fmt.Sprintf("expected code=%d, got code=%d, msg=%s", expectedCode, resp.Code, resp.Msg)
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf("%s: %v", msg, msgAndArgs)
		}
		t.Fatal(msg)
	}
}

// TestMain runs before all tests
func TestMain(m *testing.M) {
	// Check if server is running
	client := NewAPIClient()
	resp, err := client.GET("/health")
	if err != nil {
		fmt.Printf("Warning: Server not reachable at %s: %v\n", testConfig.BaseURL, err)
		fmt.Println("Please start the server before running tests:")
		fmt.Println("  go run cmd/server/main.go")
		os.Exit(1)
	}
	if !resp.IsSuccess() {
		fmt.Printf("Warning: Health check failed: code=%d, msg=%s\n", resp.Code, resp.Msg)
		os.Exit(1)
	}

	fmt.Printf("Running tests against %s\n", testConfig.BaseURL)
	os.Exit(m.Run())
}

// generateUserId generates a unique user ID for testing
func generateUserId(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// generateClientMsgId generates a unique client message ID
func generateClientMsgId() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
