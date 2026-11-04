package github

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// PR is one pull request with the data of its header and overview.
type PR struct {
	Number               int
	Title, URL           string
	State                string // OPEN, MERGED or CLOSED
	Body, Author         string
	HeadRef, BaseRef     string
	HeadSHA              string
	Additions, Deletions int
	ChangedFiles         int
	IsDraft              bool
	Mergeable            string
	// ReviewDecision is gh's: APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or "".
	ReviewDecision string
	Timeline       []Event
	ChecksSummary  CheckSummary
}

// Event is one row of the PR timeline.
type Event struct {
	Kind string // commented, reviewed, committed, merged or closed
	Who  string
	Text string
	At   time.Time
}

// CheckSummary counts the checks of the head commit by result.
type CheckSummary struct{ Pass, Fail, Pending, Skip int }

const prFields = "number,title,url,state,body,author,headRefName,baseRefName,headRefOid," +
	"additions,deletions,changedFiles,isDraft,mergeable,comments,reviews,commits," +
	"statusCheckRollup,mergedAt,closedAt,reviewDecision"

type login struct {
	Login string `json:"login"`
}

type rawPR struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	URL          string     `json:"url"`
	State        string     `json:"state"`
	Body         string     `json:"body"`
	Author       login      `json:"author"`
	HeadRef      string     `json:"headRefName"`
	BaseRef      string     `json:"baseRefName"`
	HeadSHA      string     `json:"headRefOid"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changedFiles"`
	IsDraft      bool       `json:"isDraft"`
	Mergeable    string     `json:"mergeable"`
	Decision     string     `json:"reviewDecision"`
	MergedAt     *time.Time `json:"mergedAt"`
	ClosedAt     *time.Time `json:"closedAt"`
	Comments     []struct {
		Author    login     `json:"author"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"createdAt"`
	} `json:"comments"`
	Reviews []struct {
		Author      login     `json:"author"`
		Body        string    `json:"body"`
		State       string    `json:"state"`
		SubmittedAt time.Time `json:"submittedAt"`
	} `json:"reviews"`
	Commits []struct {
		Headline string    `json:"messageHeadline"`
		Date     time.Time `json:"committedDate"`
		Authors  []struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"authors"`
	} `json:"commits"`
	Rollup []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
	} `json:"statusCheckRollup"`
}

// PR reads pull request n.
func (c Client) PR(ctx context.Context, n int) (PR, error) {
	out, err := c.run(ctx, "pr", "view", fmt.Sprint(n), "--json", prFields)
	if err != nil {
		return PR{}, err
	}
	var r rawPR
	if err := json.Unmarshal(out, &r); err != nil {
		return PR{}, fmt.Errorf("gh: read pr %d: %w", n, err)
	}
	pr := PR{
		Number: r.Number, Title: r.Title, URL: r.URL, State: r.State,
		Body: r.Body, Author: r.Author.Login, HeadRef: r.HeadRef,
		BaseRef: r.BaseRef, HeadSHA: r.HeadSHA, Additions: r.Additions,
		Deletions: r.Deletions, ChangedFiles: r.ChangedFiles,
		IsDraft: r.IsDraft, Mergeable: r.Mergeable, ReviewDecision: r.Decision,
	}
	for _, cm := range r.Comments {
		pr.Timeline = append(pr.Timeline, Event{"commented", cm.Author.Login, cm.Body, cm.CreatedAt})
	}
	for _, rv := range r.Reviews {
		pr.Timeline = append(pr.Timeline, Event{"reviewed", rv.Author.Login, rv.State, rv.SubmittedAt})
	}
	for _, co := range r.Commits {
		who := ""
		if len(co.Authors) > 0 {
			who = co.Authors[0].Login
			if who == "" {
				who = co.Authors[0].Name
			}
		}
		pr.Timeline = append(pr.Timeline, Event{"committed", who, co.Headline, co.Date})
	}
	if r.MergedAt != nil {
		pr.Timeline = append(pr.Timeline, Event{"merged", "", "", *r.MergedAt})
	} else if r.ClosedAt != nil {
		pr.Timeline = append(pr.Timeline, Event{"closed", "", "", *r.ClosedAt})
	}
	sort.SliceStable(pr.Timeline, func(i, j int) bool { return pr.Timeline[i].At.Before(pr.Timeline[j].At) })
	for _, k := range r.Rollup {
		switch rollupResult(k.Status, k.Conclusion, k.State) {
		case "pass":
			pr.ChecksSummary.Pass++
		case "fail":
			pr.ChecksSummary.Fail++
		case "skip":
			pr.ChecksSummary.Skip++
		default:
			pr.ChecksSummary.Pending++
		}
	}
	return pr, nil
}

// rollupResult maps a check run (status, conclusion) or a commit status
// (state) to pass, fail, skip or pending.
func rollupResult(status, conclusion, state string) string {
	if state != "" { // commit status
		switch state {
		case "SUCCESS":
			return "pass"
		case "FAILURE", "ERROR":
			return "fail"
		}
		return "pending"
	}
	if status != "COMPLETED" {
		return "pending"
	}
	switch conclusion {
	case "SUCCESS":
		return "pass"
	case "SKIPPED", "NEUTRAL":
		return "skip"
	}
	return "fail"
}

// BranchPR returns the number of the open pull request of the checked-out
// branch of c.Dir, from `gh pr view --json number,state`. It returns 0 and no
// error when the branch has no pull request, or its pull request is closed or
// merged (D104).
func (c Client) BranchPR(ctx context.Context) (int, error) {
	out, err := c.run(ctx, "pr", "view", "--json", "number,state")
	if err != nil {
		if strings.Contains(err.Error(), "no pull requests found") {
			return 0, nil
		}
		return 0, err
	}
	var r struct {
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return 0, fmt.Errorf("gh: read branch pr: %w", err)
	}
	if r.State == "CLOSED" || r.State == "MERGED" {
		return 0, nil
	}
	return r.Number, nil
}

// OpenIssue opens issue n of the repository of c.Dir in the web browser.
func (c Client) OpenIssue(ctx context.Context, n int) error {
	_, err := c.run(ctx, "issue", "view", fmt.Sprint(n), "--web")
	return err
}
