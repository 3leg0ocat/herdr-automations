package cleanup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DnzzL/herdr-automations/internal/herdr"
	"github.com/DnzzL/herdr-automations/internal/hostpath"
)

// hcRepos is the adapter for the desktop app (backend: hc). The app's spaces
// live in herdr-core, so "still open" means a live hc terminal whose folder is
// inside the worktree: closing the run's space in the app ends its terminals.
//
// "Removable" is also stricter than gitRepos': no commit on the run branch may
// be missing from every other branch and remote. A run that committed nothing
// passes whatever the default branch is called, and one whose work was pushed
// or merged passes too; anything else stays for a person to read.
type hcRepos struct{}

func (hcRepos) Worktrees(repo string) ([]herdr.Worktree, error) {
	live, err := liveFolders()
	if err != nil {
		// Not knowing which spaces are open must never read as "none are".
		return nil, fmt.Errorf("asking herdr-core which terminals are live: %w", err)
	}
	out, err := output(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []herdr.Worktree
	for _, w := range parsePorcelain(out) {
		if inUse(w.Path, live) {
			w.OpenWorkspaceID = "live terminal"
		}
		list = append(list, w)
	}
	return list, nil
}

// DefaultBranch is not needed: Merged asks about every branch at once.
func (hcRepos) DefaultBranch(string) (string, error) { return "", nil }

func (hcRepos) Merged(repo, branch, _ string) bool { return uniqueCommits(repo, branch) == 0 }

func (hcRepos) Dirty(path string) bool { return dirty(path) }

// Remove drops the checkout, never forced, so git still refuses a dirty one.
// The branch goes with -D: -d asks about the current HEAD only, and Merged has
// already shown that every commit on it is held elsewhere. It is checked again
// here, in case the agent was started again in the meantime.
func (hcRepos) Remove(c Candidate) error {
	if err := git(c.Repo, "worktree", "remove", c.Path); err != nil {
		return fmt.Errorf("%s: %w", c.Branch, err)
	}
	if n := uniqueCommits(c.Repo, c.Branch); n != 0 {
		return fmt.Errorf("%s: checkout removed, branch kept: it has commits found nowhere else", c.Branch)
	}
	if err := git(c.Repo, "branch", "-D", c.Branch); err != nil {
		return fmt.Errorf("%s: checkout removed, branch kept: %w", c.Branch, err)
	}
	return nil
}

// uniqueCommits counts the commits on branch that no other branch or remote
// has; -1 when git cannot say.
func uniqueCommits(repo, branch string) int {
	// --exclude takes the name as --branches sees it, without refs/heads/.
	out, err := output(repo, "rev-list", "--count", "refs/heads/"+branch, "--not",
		"--exclude="+branch, "--branches", "--remotes")
	if err != nil {
		return -1
	}
	var n int
	if _, err := fmt.Sscan(out, &n); err != nil {
		return -1
	}
	return n
}

// parsePorcelain reads `git worktree list --porcelain`: blank-line separated
// records of "worktree <path>" and "branch refs/heads/<name>".
func parsePorcelain(out string) []herdr.Worktree {
	var list []herdr.Worktree
	var cur *herdr.Worktree
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			list = append(list, herdr.Worktree{Path: filepath.FromSlash(strings.TrimPrefix(line, "worktree "))})
			cur = &list[len(list)-1]
		case strings.HasPrefix(line, "branch ") && cur != nil:
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	for i := range list {
		list[i].IsLinkedWorktree = i > 0
	}
	return list
}

// liveFolders is every live hc terminal's current folder.
func liveFolders() ([]string, error) {
	cmd := exec.Command(hostpath.HCBin(), "ls", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var ls struct {
		Terminals []struct {
			State string `json:"state"`
			Cwd   string `json:"cwd"`
		} `json:"terminals"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &ls); err != nil {
		return nil, err
	}
	var dirs []string
	for _, t := range ls.Terminals {
		if t.State == "live" && t.Cwd != "" {
			dirs = append(dirs, t.Cwd)
		}
	}
	return dirs, nil
}

// inUse reports whether any of dirs is path or inside it.
func inUse(path string, dirs []string) bool {
	root := norm(path)
	for _, d := range dirs {
		if d := norm(d); d == root || strings.HasPrefix(d, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func norm(p string) string {
	p = filepath.Clean(filepath.FromSlash(p))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}
