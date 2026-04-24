package a2a

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Client sends outbound A2A delegation requests.
type Client struct {
	httpClient *http.Client
	db         *sql.DB
}

// NewClient creates an A2A client.
func NewClient(httpClient *http.Client, db *sql.DB) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{httpClient: httpClient, db: db}
}

// Discover fetches the agent card from a peer agent.
func (c *Client) Discover(ctx context.Context, agentURL string) (*AgentCard, error) {
	url := strings.TrimRight(agentURL, "/") + "/.well-known/agent.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("a2a: discover: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a: discover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a: discover: status %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("a2a: discover: decode: %w", err)
	}
	return &card, nil
}

// Delegate sends a task to a peer agent.
func (c *Client) Delegate(ctx context.Context, agentURL, token, task string) (*TaskResult, error) {
	url := strings.TrimRight(agentURL, "/") + "/a2a/tasks"

	taskReq := TaskRequest{
		ID:   fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Task: task,
	}
	body, err := json.Marshal(taskReq)
	if err != nil {
		return nil, fmt.Errorf("a2a: delegate: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("a2a: delegate: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Record outbound delegation.
	var delegationID int64
	if c.db != nil {
		var recErr error
		delegationID, recErr = c.recordOutbound(ctx, agentURL, task)
		if recErr != nil {
			log.Printf("a2a: recordOutbound: %v", recErr)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.db != nil && delegationID > 0 {
			c.completeOutbound(ctx, delegationID, "failed", err.Error())
		}
		return nil, fmt.Errorf("a2a: delegate: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("a2a: delegate: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if c.db != nil && delegationID > 0 {
			c.completeOutbound(ctx, delegationID, "failed", string(respBody))
		}
		return nil, fmt.Errorf("a2a: delegate: status %d: %s", resp.StatusCode, respBody)
	}

	var result TaskResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("a2a: delegate: decode: %w", err)
	}

	if c.db != nil && delegationID > 0 {
		c.completeOutbound(ctx, delegationID, result.Status, truncateResult(result.Output))
	}

	return &result, nil
}

func (c *Client) recordOutbound(ctx context.Context, peer, task string) (int64, error) {
	res, err := c.db.ExecContext(ctx,
		`INSERT INTO a2a_delegations (direction, peer_agent, task_summary, status, created_at)
		 VALUES ('outbound', ?, ?, 'pending', ?)`,
		peer, truncateResult(task), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (c *Client) completeOutbound(ctx context.Context, id int64, status, result string) {
	_, err := c.db.ExecContext(ctx,
		`UPDATE a2a_delegations SET status = ?, result_summary = ?, completed_at = ? WHERE id = ?`,
		status, result, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		log.Printf("a2a: completeOutbound: %v", err)
	}
}
