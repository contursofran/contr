package git

import "context"

// Init makes dir a git repository with an empty first commit, so worktrees
// have a base (D233). It stops at the first git error.
func Init(ctx context.Context, r Runner, dir string) error {
	if _, err := r.Run(ctx, dir, "init"); err != nil {
		return err
	}
	_, err := r.Run(ctx, dir, "commit", "--allow-empty", "-m", "Initial commit")
	return err
}
