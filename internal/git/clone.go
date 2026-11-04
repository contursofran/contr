package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrDestExists means the clone destination is already on disk.
var ErrDestExists = errors.New("git: destination already exists")

// ErrGitNotFound is the error of FindGit when git is on no searched path.
var ErrGitNotFound = errors.New("git: program not found on PATH or in /usr/bin, /opt/homebrew/bin, /usr/local/bin")

var (
	ownerRepoRE = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)/([A-Za-z0-9._-]+)$`)
	httpsRE     = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(?::[0-9]+)?/[A-Za-z0-9._~/-]+$`)
	sshRE       = regexp.MustCompile(`^git@[A-Za-z0-9.-]+:[A-Za-z0-9._~/-]+$`)
)

// ParseRemote turns what the owner typed into a clone URL and the folder name
// of the clone. It accepts `owner/repo` (a GitHub repository), an `https://`
// URL and a `git@host:path` URL. It rejects empty input, spaces and `..`.
func ParseRemote(input string) (url, name string, err error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return "", "", errors.New("git: empty remote")
	}
	if strings.ContainsAny(in, " \t\r\n") || strings.Contains(in, "..") {
		return "", "", fmt.Errorf("git: bad remote %q", input)
	}
	switch {
	case ownerRepoRE.MatchString(in):
		m := ownerRepoRE.FindStringSubmatch(in)
		name = strings.TrimSuffix(m[2], ".git")
		url = "https://github.com/" + m[1] + "/" + name + ".git"
	case httpsRE.MatchString(in), sshRE.MatchString(in):
		url = in
		base := in[strings.LastIndexAny(in, "/:")+1:]
		name = strings.TrimSuffix(base, ".git")
	default:
		return "", "", fmt.Errorf("git: bad remote %q", input)
	}
	if name == "" || name == "." {
		return "", "", fmt.Errorf("git: bad remote %q", input)
	}
	return url, name, nil
}

// CloneRunner runs git with args and gives its stderr text. Tests use a fake.
type CloneRunner func(ctx context.Context, git string, args ...string) (stderr string, err error)

// FindGit returns the git program: "git" from PATH, else the first git in the
// usual places. An app opened from Finder has only the short launchd PATH,
// which can lack Homebrew's git. dirs, when not nil, replaces the usual places.
func FindGit(dirs []string) (string, error) {
	if p, err := exec.LookPath("git"); err == nil {
		return p, nil
	}
	if dirs == nil {
		dirs = []string{"/usr/bin", "/opt/homebrew/bin", "/usr/local/bin"}
	}
	for _, d := range dirs {
		p := filepath.Join(d, "git")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", ErrGitNotFound
}

// cloneWaitDelay is how long ExecClone waits for git's pipes after it kills
// git on cancel. A child of git can keep the pipe open.
const cloneWaitDelay = 2 * time.Second

// ExecClone is the CloneRunner that starts the program. It turns off git's
// prompts, so a private repository fails at once instead of waiting for input.
func ExecClone(ctx context.Context, git string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = cloneWaitDelay
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		// git exited well; a child of git kept the pipe open.
		err = nil
	}
	return stderr.String(), err
}

// Clone runs `git clone url dest`. It refuses a dest that exists and does not
// call run then. On failure the error text is the last line of stderr. It
// blocks: call it only from a goroutine. A nil run means ExecClone. When git
// is not found, it returns that error.
func Clone(ctx context.Context, run CloneRunner, url, dest string) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%w: %s", ErrDestExists, dest)
	}
	if run == nil {
		run = ExecClone
	}
	git, err := FindGit(nil)
	if err != nil {
		return err
	}
	stderr, err := run(ctx, git, "clone", "--", url, dest)
	if err != nil {
		if line := lastLine(stderr); line != "" {
			return errors.New(line)
		}
		return err
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(s, "\r", "\n")), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
