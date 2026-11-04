package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Thread is one review thread: comments on a line of a file.
type Thread struct {
	ID, Path string
	// Line is the line of the new file (Side RIGHT) or the old file (LEFT).
	// For an outdated thread it is the original line.
	Line     int
	Side     string // LEFT or RIGHT
	Resolved bool
	Comments []Comment
}

// Comment is one comment of a thread.
type Comment struct {
	ID, Who, Body string
	At            time.Time
}

const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){
repository(owner:$owner,name:$name){pullRequest(number:$number){
reviewThreads(first:100){nodes{id isResolved path line originalLine diffSide
comments(first:50){nodes{id author{login} body createdAt}}}}}}}`

// Threads returns the review threads of pull request n, comments in the
// order of GitHub. The owner and name of the repository come from gh.
func (c Client) Threads(ctx context.Context, n int) ([]Thread, error) {
	out, err := c.run(ctx, "repo", "view", "--json", "owner,name")
	if err != nil {
		return nil, err
	}
	var repo struct {
		Owner login  `json:"owner"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(out, &repo); err != nil {
		return nil, fmt.Errorf("gh: read repo: %w", err)
	}
	if repo.Owner.Login == "" || repo.Name == "" {
		return nil, errors.New("gh: repo view has no owner or name")
	}
	out, err = c.run(ctx, "api", "graphql", "-f", "query="+threadsQuery,
		"-F", "owner="+repo.Owner.Login, "-F", "name="+repo.Name, "-F", "number="+strconv.Itoa(n))
	if err != nil {
		return nil, err
	}
	var raw struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			Repository struct {
				PullRequest *struct {
					ReviewThreads struct {
						Nodes []struct {
							ID           string `json:"id"`
							IsResolved   bool   `json:"isResolved"`
							Path         string `json:"path"`
							Line         *int   `json:"line"`
							OriginalLine *int   `json:"originalLine"`
							DiffSide     string `json:"diffSide"`
							Comments     struct {
								Nodes []struct {
									ID        string    `json:"id"`
									Author    *login    `json:"author"`
									Body      string    `json:"body"`
									CreatedAt time.Time `json:"createdAt"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh: read threads: %w", err)
	}
	if len(raw.Errors) > 0 {
		msgs := make([]string, len(raw.Errors))
		for i, e := range raw.Errors {
			msgs[i] = e.Message
		}
		return nil, errors.New("gh: " + strings.Join(msgs, "; "))
	}
	pr := raw.Data.Repository.PullRequest
	if pr == nil {
		return nil, fmt.Errorf("gh: pr %d not found", n)
	}
	threads := make([]Thread, 0, len(pr.ReviewThreads.Nodes))
	for _, nd := range pr.ReviewThreads.Nodes {
		t := Thread{ID: nd.ID, Path: nd.Path, Side: nd.DiffSide, Resolved: nd.IsResolved}
		switch {
		case nd.Line != nil:
			t.Line = *nd.Line
		case nd.OriginalLine != nil:
			t.Line = *nd.OriginalLine
		}
		for _, cm := range nd.Comments.Nodes {
			who := ""
			if cm.Author != nil {
				who = cm.Author.Login
			}
			t.Comments = append(t.Comments, Comment{ID: cm.ID, Who: who, Body: cm.Body, At: cm.CreatedAt})
		}
		threads = append(threads, t)
	}
	return threads, nil
}
