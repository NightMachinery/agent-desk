// Package keeper talks to the local agent-quotas daemon.
package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

var ErrUnavailable = errors.New("agent-quotas is not running")

const (
	readTimeout = 15 * time.Second // keeper reads can take several seconds
	// listTimeout is longer: the list is read in the background, and the
	// keeper can take several seconds to build it.
	listTimeout = 30 * time.Second
)

type Client struct {
	Socket string
	Home   string // Paseo home
}

type Agent struct {
	ID             string   `json:"id"`
	Big            bool     `json:"big"`
	CacheExpiresAt *float64 `json:"cache_expires_at"`
	WarmUntil      *float64 `json:"warm_until"`
	Archived       bool     `json:"archived"`
	Cache          *Cache   `json:"cache"`
	LockPath       string   `json:"lock_path"`
}

func (a Agent) Cold(now time.Time) bool {
	return a.Big && a.CacheExpiresAt != nil && *a.CacheExpiresAt < float64(now.Unix())+float64(now.Nanosecond())/1e9
}

type Cache struct {
	Policy     string      `json:"policy"`
	Phase      string      `json:"phase"`
	WarmHours  float64     `json:"warm_hours"`
	Compaction *Compaction `json:"compaction"`
}

type Compaction struct {
	ID          string  `json:"id"`
	State       string  `json:"state"`
	Error       string  `json:"error"`
	RequestedAt float64 `json:"requested_at"`
}

type APIError struct {
	Status  int
	Message string
}

func (e APIError) Error() string { return "agent-quotas: " + e.Message }

type PolicyRequest struct {
	Cache     string  `json:"cache,omitempty"`
	WarmHours float64 `json:"warm_hours,omitempty"`
	SetBy     string  `json:"set_by"`
}

type List struct {
	Enabled bool    `json:"keeper_enabled"`
	Agents  []Agent `json:"agents"`
}

func (c Client) Agents(ctx context.Context, all bool) (List, error) {
	var result List
	q := url.Values{}
	if all {
		q.Set("all", "1")
	}
	err := c.request(ctx, listTimeout, http.MethodGet, "", q, nil, &result)
	return result, err
}

func (c Client) Agent(ctx context.Context, id string) (Agent, error) {
	return c.agent(ctx, readTimeout, http.MethodGet, agentPath(id), nil, nil)
}

func (c Client) Policy(ctx context.Context, id, action string, value PolicyRequest) (Agent, error) {
	return c.agent(ctx, 15*time.Second, http.MethodPost, agentPath(id)+"/"+action, nil, value)
}

func (c Client) Compact(ctx context.Context, id string) (Compaction, error) {
	var result struct {
		ID    string `json:"compaction_id"`
		State string `json:"state"`
	}
	err := c.request(ctx, 30*time.Second, http.MethodPost, agentPath(id)+"/compact", nil, nil, &result)
	return Compaction{ID: result.ID, State: result.State}, err
}

func (c Client) Wait(ctx context.Context, id, compactionID string) (Agent, error) {
	q := url.Values{"wait_compaction": {compactionID}, "timeout": {"60"}}
	return c.agent(ctx, 65*time.Second, http.MethodGet, agentPath(id), q, nil)
}

func agentPath(id string) string { return "/" + url.PathEscape(id) }

func (c Client) agent(ctx context.Context, timeout time.Duration, method, path string, q url.Values, body any) (Agent, error) {
	var result struct {
		Agent Agent `json:"agent"`
	}
	err := c.request(ctx, timeout, method, path, q, body, &result)
	return result.Agent, err
}

func (c Client) request(ctx context.Context, timeout time.Duration, method, path string, q url.Values, body any, result any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("home", c.Home)
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://keeper/v1/agents"+path+"?"+q.Encode(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	dialer := net.Dialer{Timeout: time.Second}
	client := http.Client{Timeout: timeout, Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", c.Socket)
		},
	}}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return ErrUnavailable
		}
		return fmt.Errorf("agent-quotas request: %w", err)
	}
	defer res.Body.Close()
	data := json.NewDecoder(io.LimitReader(res.Body, 16<<20))
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if data.Decode(&failure) != nil || failure.Error == "" {
			failure.Error = res.Status
		}
		return APIError{Status: res.StatusCode, Message: failure.Error}
	}
	return data.Decode(result)
}
