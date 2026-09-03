// Package github is a minimal REST client for PR comments, labels, and artifacts.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

var (
	ErrArtifactNotFound = errors.New("artifact not found")
	ErrLabelForbidden   = errors.New("label creation forbidden")
)

type Client struct {
	BaseURL string
	Token   string
	Repo    string
	HTTP    *http.Client
}

func New(repo, token string) *Client {
	return &Client{BaseURL: "https://api.github.com", Token: token, Repo: repo, HTTP: http.DefaultClient}
}

func Token() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("github api: %d %s", e.Status, e.Body) }

func (c *Client) do(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	url := path
	if !strings.HasPrefix(path, "http") {
		url = c.BaseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &httpError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func statusIs(err error, code int) bool {
	var he *httpError
	return errors.As(err, &he) && he.Status == code
}
