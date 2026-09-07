package coolify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiPrefix = "/api/v1"
)

type Client struct {
	baseUrl string
	token   string
	http    *http.Client
	team    string
}

func New(baseUrl, token string) *Client {
	return &Client{
		baseUrl: strings.TrimRight(baseUrl, "/"),
		token:   token,
		http:    &http.Client{Timeout: time.Second * 30},
		team:    "",
	}
}

// SetTeam sets the team for the client up front, so 404's can name it.
func (c *Client) SetTeam(name string, id int) {
	c.team = fmt.Sprintf("%s (%d)", name, id)
}

func (c *Client) CurrentTeam(ctx context.Context) (Team, error) {
	var team Team
	err := c.get(ctx, "/teams/current", &team)
	if err != nil {
		return Team{}, err
	}
	return team, nil
}

// VerifyTeam checks which team the token acts as and refuses when it is not
// the expected one, so a mis-scoped token is caught before anything happens.
// Acting commands call this on every run.
func (c *Client) VerifyTeam(ctx context.Context, teamID int) (Team, error) {
	team, err := c.CurrentTeam(ctx)
	if err != nil {
		return Team{}, err
	}
	if team.Id != teamID {
		return Team{}, fmt.Errorf(
			"the token acts as team %q (id %d), but berth is configured for team id %d — check BERTH_TOKEN or the team in your config",
			team.Name, team.Id, teamID)
	}
	return team, nil
}

type Team struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if c.token == "" {
		return ErrNoToken
	}

	var rdr io.Reader
	if body != nil {
		enc, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(enc)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.createUrl(path), rdr)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("cannot reach %s: %w", c.baseUrl, urlErr)
		}
		return fmt.Errorf("cannot reach %s: %w", c.baseUrl, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cannot read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp, raw, c.team)
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("cannot decode %s %s %w", method, path, err)
		}
	}

	return nil
}

func (c *Client) createUrl(path string) string {
	return fmt.Sprintf("%s%s%s", c.baseUrl, apiPrefix, path)
}
