package cleanup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePorcelainReadsPathsAndBranches(t *testing.T) {
	out := "worktree C:/r\nHEAD 1\nbranch refs/heads/main\n\nworktree C:/w/auto-x\nHEAD 2\nbranch refs/heads/auto/x-1\n\nworktree C:/w/detached\nHEAD 3\ndetached\n"
	got := parsePorcelain(out)
	if len(got) != 3 {
		t.Fatalf("got %d worktrees, want 3: %+v", len(got), got)
	}
	if got[1].Branch != "auto/x-1" || got[1].Path != filepath.FromSlash("C:/w/auto-x") || !got[1].IsLinkedWorktree {
		t.Errorf("second worktree = %+v", got[1])
	}
	if got[0].IsLinkedWorktree || got[2].Branch != "" {
		t.Errorf("main checkout or detached one misread: %+v", got)
	}
}

func TestInUseMatchesTheFolderAndBelowOnly(t *testing.T) {
	root := filepath.FromSlash("/w/auto-x")
	for dir, want := range map[string]bool{
		"/w/auto-x":       true,
		"/w/auto-x/src":   true,
		"/w/auto-x-other": false,
		"/w":              false,
	} {
		if got := inUse(root, []string{filepath.FromSlash(dir)}); got != want {
			t.Errorf("inUse(%s, %s) = %v, want %v", root, dir, got, want)
		}
	}
}

// A real repository: a run branch with no commit of its own goes, one with a
// commit nowhere else stays, and so does one with uncommitted changes.
func TestHCReposJudgesRealWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run(repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", ".")
	run(repo, "commit", "-qm", "init")

	wt := t.TempDir()
	idle, work, messy := filepath.Join(wt, "idle"), filepath.Join(wt, "work"), filepath.Join(wt, "messy")
	run(repo, "worktree", "add", "-q", "-b", "auto/idle", idle, "HEAD")
	run(repo, "worktree", "add", "-q", "-b", "auto/work", work, "HEAD")
	run(repo, "worktree", "add", "-q", "-b", "auto/messy", messy, "HEAD")
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", ".")
	run(work, "commit", "-qm", "work")
	if err := os.WriteFile(filepath.Join(messy, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := hcRepos{}
	for path, want := range map[string]bool{idle: false, work: false, messy: true} {
		if got := r.Dirty(path); got != want {
			t.Errorf("Dirty(%s) = %v, want %v", filepath.Base(path), got, want)
		}
	}
	if !r.Merged(repo, "auto/idle", "") {
		t.Error("a branch with no commit of its own should count as landed")
	}
	if r.Merged(repo, "auto/work", "") {
		t.Error("a branch with a commit nowhere else must be kept")
	}
	if err := r.Remove(Candidate{Repo: repo, Branch: "auto/idle", Path: idle}); err != nil {
		t.Fatalf("Remove idle: %v", err)
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Errorf("idle worktree still on disk: %v", err)
	}
	if out, _ := output(repo, "branch", "--list", "auto/idle"); out != "" {
		t.Errorf("auto/idle branch still there: %q", out)
	}
	if err := r.Remove(Candidate{Repo: repo, Branch: "auto/messy", Path: messy}); err == nil {
		t.Error("removing a dirty worktree must fail, not force")
	}
}
