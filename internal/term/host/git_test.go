package host

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadGit(t *testing.T) {
	top := t.TempDir()
	repo := filepath.Join(top, "repo")
	write(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	os.MkdirAll(filepath.Join(repo, "src", "deep"), 0o755)
	// A linked worktree, as git worktree add makes it: relative to the
	// worktree's git folder.
	wt := filepath.Join(top, "repo-feature")
	wtGit := filepath.Join(repo, ".git", "worktrees", "repo-feature")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+wtGit+"\n")
	write(t, filepath.Join(wtGit, "HEAD"), "ref: refs/heads/feat/x\n")
	write(t, filepath.Join(wtGit, "commondir"), "../..\n")
	// A submodule, its git folder in the repository's, relative.
	sub := filepath.Join(repo, "vendor", "lib")
	write(t, filepath.Join(sub, ".git"), "gitdir: ../../.git/modules/lib\n")
	write(t, filepath.Join(repo, ".git", "modules", "lib", "HEAD"), "0123456789abcdef0123456789abcdef01234567\n")
	plain := filepath.Join(top, "plain")
	os.MkdirAll(plain, 0o755)
	// The temporary directory can itself be in a checkout (a CI run's, in
	// its working copy): a plain folder then belongs to that one, and to
	// none otherwise.
	outside := readGit(filepath.Dir(top))

	for _, c := range []struct {
		dir  string
		want Git
	}{
		{repo, Git{Root: repo, Branch: "main"}},
		{filepath.Join(repo, "src", "deep"), Git{Root: repo, Branch: "main"}},
		{wt, Git{Root: wt, Branch: "feat/x", Worktree: true, Repo: repo}},
		{sub, Git{Root: sub, Branch: "0123456"}},
		{plain, outside},
		{"", Git{}},
		{"relative/dir", Git{}},
	} {
		if got := readGit(c.dir); got != c.want {
			t.Errorf("readGit(%q) = %+v, want %+v", c.dir, got, c.want)
		}
	}
}

func TestBranchOf(t *testing.T) {
	for head, want := range map[string]string{
		"ref: refs/heads/main\n":       "main",
		"ref: refs/heads/fix/a-b":      "fix/a-b",
		"ref: refs/remotes/origin/x\n": "remotes/origin/x",
		"abcdef1234567890\n":           "abcdef1",
		"":                             "",
	} {
		if got := branchOf(head); got != want {
			t.Errorf("branchOf(%q) = %q, want %q", head, got, want)
		}
	}
}
