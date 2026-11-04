package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Check is one check run of the head commit of a PR.
type Check struct {
	Name, Workflow, Link string
	// Result is pass, fail, pending, skip or cancel.
	Result             string
	Started, Completed time.Time
}

// Duration is how long the check ran. It is zero when the check has not
// started or has not finished.
func (c Check) Duration() time.Duration {
	if c.Started.IsZero() || c.Completed.IsZero() || c.Completed.Before(c.Started) {
		return 0
	}
	return c.Completed.Sub(c.Started)
}

type rawCheck struct {
	Name        string    `json:"name"`
	Workflow    string    `json:"workflow"`
	Link        string    `json:"link"`
	Bucket      string    `json:"bucket"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// Checks reads the checks of PR n. A gh exit with pending checks (code 8)
// is not an error: the rows are valid. A PR without checks gives an empty
// list.
func (c Client) Checks(ctx context.Context, n int) ([]Check, error) {
	out, err := c.run(ctx, "pr", "checks", strconv.Itoa(n),
		"--json", "name,state,bucket,startedAt,completedAt,link,workflow")
	if err != nil {
		if strings.Contains(err.Error(), "no checks reported") {
			return []Check{}, nil
		}
		// Only a failed gh call with parsable rows is accepted, and
		// never a time-out.
		if ctx.Err() != nil || len(out) == 0 {
			return nil, err
		}
	}
	var raw []rawCheck
	if jerr := json.Unmarshal(out, &raw); jerr != nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("gh: checks: %w", jerr)
	}
	res := make([]Check, 0, len(raw))
	for _, r := range raw {
		res = append(res, Check{
			Name: r.Name, Workflow: r.Workflow, Link: r.Link,
			Result:  checkResult(r.Bucket),
			Started: r.StartedAt, Completed: r.CompletedAt,
		})
	}
	return res, nil
}

func checkResult(bucket string) string {
	switch bucket {
	case "pass", "fail", "pending", "cancel":
		return bucket
	case "skipping":
		return "skip"
	}
	return "pending"
}
