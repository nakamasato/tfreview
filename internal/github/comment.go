package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/nakamasato/tfreview/internal/render"
)

type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// Find our own comment by its marker and replace it, so pushes don't pile up new comments.
func (c *Client) UpsertComment(ctx context.Context, pr int, body string) error {
	for page := 1; ; page++ {
		var comments []issueComment
		path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", c.Repo, pr, page)
		if err := c.do(ctx, "GET", path, nil, &comments); err != nil {
			return err
		}
		for _, cm := range comments {
			// Only a comment that IS ours (starts with our marker) gets replaced.
			// A comment that merely contains the marker somewhere in its body is
			// someone else's text quoting or discussing ours, not our comment.
			if strings.HasPrefix(strings.TrimSpace(cm.Body), render.Begin) {
				return c.do(ctx, "PATCH", fmt.Sprintf("/repos/%s/issues/comments/%d", c.Repo, cm.ID), map[string]string{"body": body}, nil)
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	return c.do(ctx, "POST", fmt.Sprintf("/repos/%s/issues/%d/comments", c.Repo, pr), map[string]string{"body": body}, nil)
}

type label struct {
	Name string `json:"name"`
}

const labelPrefix = "tfreview:"

func (c *Client) SetLabel(ctx context.Context, pr int, name string) error {
	var current []label
	if err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/issues/%d/labels", c.Repo, pr), nil, &current); err != nil {
		return err
	}
	has := false
	for _, l := range current {
		if !strings.HasPrefix(l.Name, labelPrefix) {
			continue
		}
		if l.Name == name {
			has = true
			continue
		}
		if err := c.do(ctx, "DELETE", fmt.Sprintf("/repos/%s/issues/%d/labels/%s", c.Repo, pr, url.PathEscape(l.Name)), nil, nil); err != nil && !statusIs(err, 404) {
			return err
		}
	}
	if has {
		return nil
	}
	add := func() error {
		return c.do(ctx, "POST", fmt.Sprintf("/repos/%s/issues/%d/labels", c.Repo, pr), map[string][]string{"labels": {name}}, nil)
	}
	err := add()
	if !statusIs(err, 404) {
		return err
	}
	create := map[string]string{"name": name, "color": render.LabelColor(name), "description": "tfreview verdict"}
	if err := c.do(ctx, "POST", fmt.Sprintf("/repos/%s/labels", c.Repo), create, nil); err != nil {
		if statusIs(err, 403) {
			return fmt.Errorf("%w: %v", ErrLabelForbidden, err)
		}
		return err
	}
	return add()
}
