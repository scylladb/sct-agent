package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/scylladb/sct-agent/internal/storage"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) ExecuteCommand(ctx context.Context, req *storage.ExecuteRequest) (*storage.ExecuteResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/commands", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp)
	}

	var result storage.ExecuteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *Client) GetJob(ctx context.Context, jobID string) (*storage.Job, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/commands/%s", jobID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp)
	}

	var job storage.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &job, nil
}

func (c *Client) WaitForJob(ctx context.Context, jobID string, pollInterval time.Duration) (*storage.Job, error) {
	if pollInterval == 0 {
		pollInterval = time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			job, err := c.GetJob(ctx, jobID)
			if err != nil {
				return nil, err
			}

			switch job.Status {
			case storage.StatusCompleted, storage.StatusFailed, storage.StatusCancelled:
				return job, nil
			case storage.StatusQueued, storage.StatusRunning:
				continue
			default:
				return nil, fmt.Errorf("unknown job status: %s", job.Status)
			}
		}
	}
}

func (c *Client) ExecuteAndWait(ctx context.Context, req *storage.ExecuteRequest, pollInterval time.Duration) (*storage.Job, error) {
	jobResp, err := c.ExecuteCommand(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.WaitForJob(ctx, jobResp.JobID, pollInterval)
}

func (c *Client) CancelJob(ctx context.Context, jobID string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/commands/%s", jobID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleErrorResponse(resp)
	}

	return nil
}

type ListJobsOptions struct {
	Status string
	Limit  int
	Offset int
	Since  *time.Time
}

func (c *Client) ListJobs(ctx context.Context, opts *ListJobsOptions) (*storage.JobListResponse, error) {
	params := url.Values{}

	if opts != nil {
		if opts.Status != "" {
			params.Set("status", opts.Status)
		}
		if opts.Limit > 0 {
			params.Set("limit", strconv.Itoa(opts.Limit))
		}
		if opts.Offset > 0 {
			params.Set("offset", strconv.Itoa(opts.Offset))
		}
		if opts.Since != nil {
			params.Set("since", opts.Since.Format(time.RFC3339))
		}
	}

	path := "/api/v1/commands"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp)
	}

	var result storage.JobListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *Client) Health(ctx context.Context) (*storage.HealthResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp)
	}

	var health storage.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &health, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if path != "/health" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	if method == http.MethodPost && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HTTP %d: unable to read error response", resp.StatusCode)
	}

	var errorResp storage.ErrorResponse
	if jsonErr := json.Unmarshal(body, &errorResp); jsonErr != nil {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("HTTP %d: %s - %s", resp.StatusCode, errorResp.Error, errorResp.Message)
}
