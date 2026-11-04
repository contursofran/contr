package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Every write below needs a click in the view (D103). Errors start with
// "gh: ", as for reads.

const replyMutation = `mutation($id: ID!, $body: String!) {
  addPullRequestReviewThreadReply(input: {pullRequestReviewThreadId: $id, body: $body}) {
    comment { id }
  }
}`

const resolveMutation = `mutation($id: ID!) {
  resolveReviewThread(input: {threadId: $id}) {
    thread { id }
  }
}`

// Comment adds a comment to pull request n.
func (c Client) Comment(ctx context.Context, n int, body string) error {
	_, err := c.run(ctx, "pr", "comment", strconv.Itoa(n), "--body", body)
	return err
}

// Approve approves pull request n.
func (c Client) Approve(ctx context.Context, n int) error {
	_, err := c.run(ctx, "pr", "review", strconv.Itoa(n), "--approve")
	return err
}

// Reply adds a reply to the review thread with id threadID.
func (c Client) Reply(ctx context.Context, threadID, body string) error {
	return c.mutate(ctx, replyMutation, "id="+threadID, "body="+body)
}

// Resolve marks the review thread with id threadID as resolved.
func (c Client) Resolve(ctx context.Context, threadID string) error {
	return c.mutate(ctx, resolveMutation, "id="+threadID)
}

// Merge squash-merges pull request n. The merge only happens when the head of
// the pull request is still headSHA, so it never merges a commit that the
// owner has not seen.
func (c Client) Merge(ctx context.Context, n int, headSHA string) error {
	if strings.TrimSpace(headSHA) == "" {
		return errors.New("gh: merge needs the head commit")
	}
	_, err := c.run(ctx, "pr", "merge", strconv.Itoa(n), "--squash", "--match-head-commit", headSHA)
	return err
}

// Viewer returns the login of the signed-in GitHub user.
func (c Client) Viewer(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	login := strings.TrimSpace(string(out))
	if login == "" {
		return "", errors.New("gh: no login")
	}
	return login, nil
}

// mutate runs a GraphQL mutation. fields are "name=value" strings sent as
// string variables with -f, so the text is never read as a file or number.
func (c Client) mutate(ctx context.Context, query string, fields ...string) error {
	args := []string{"api", "graphql", "-f", "query=" + query}
	for _, f := range fields {
		args = append(args, "-f", f)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return err
	}
	var resp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("gh: bad graphql reply: %w", err)
	}
	if len(resp.Errors) > 0 {
		msgs := make([]string, len(resp.Errors))
		for i, e := range resp.Errors {
			msgs[i] = e.Message
		}
		return errors.New("gh: " + strings.Join(msgs, "; "))
	}
	return nil
}
