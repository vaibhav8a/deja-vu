package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vshulcz/deja-vu/internal/index"
)

// A home directory is not a project: everything ever launched from there
// shares its name, and the repository being worked on is not in that scope at
// all. Measured on a real store 2026-08-19 — an agent started from
// /Users/shulcz recalled a session about a different project into a
// conversation about this one, while the answer sat in a project the scope did
// not cover.
func TestScopeIsNotAProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	for _, tc := range []struct {
		name string
		cwd  string
		want bool
	}{
		{"the home directory itself", home, true},
		{"the home directory with a trailing slash", home + string(filepath.Separator), true},
		{"a filesystem root", string(filepath.Separator), true},
		{"nothing at all", "", true},
		{"a project inside the home directory", filepath.Join(home, "code", "proj"), false},
		{"a project elsewhere", filepath.Join(t.TempDir(), "proj"), false},
		// A directory that is not a repository is still somebody's project.
		// The rule refuses catch-alls, not un-versioned work.
		{"a plain directory", filepath.Join(home, "notes"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scopeIsNotAProject(tc.cwd); got != tc.want {
				t.Errorf("scopeIsNotAProject(%q) = %v, want %v", tc.cwd, got, tc.want)
			}
		})
	}
}

// The gate has to hold in the hook, not only in the predicate: the point is
// that nothing is injected from a catch-all scope.
func TestPromptHookStaysSilentFromTheHomeDirectory(t *testing.T) {
	tmp := hermeticEnv(t)
	root := filepath.Join(tmp, "claude", "proj-scope")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEJA_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	at := time.Now().AddDate(0, 0, -2).UTC().Format(time.RFC3339)
	body := `{"type":"user","sessionId":"sc1","cwd":"` + filepath.ToSlash(root) + `","timestamp":"` + at + `","message":{"role":"user","content":"the pgbouncer prepared statements keep failing"}}` + "\n" +
		`{"type":"assistant","sessionId":"sc1","cwd":"` + filepath.ToSlash(root) + `","timestamp":"` + at + `","message":{"role":"assistant","content":[{"type":"text","text":"decision: run pgbouncer in session mode"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "sc1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// And a session recorded while sitting in the home directory itself. This
	// is what makes the test able to fail: without the gate the hook ranks
	// this one under the home scope and injects it, because it carries the
	// same words. It is not an answer to anything — it is the neighbour a
	// catch-all scope hands back.
	home := sourcesHomeForTest()
	hroot := filepath.Join(tmp, "claude", "proj-home")
	if err := os.MkdirAll(hroot, 0o755); err != nil {
		t.Fatal(err)
	}
	hbody := `{"type":"user","sessionId":"hm1","cwd":"` + filepath.ToSlash(home) + `","timestamp":"` + at + `","message":{"role":"user","content":"unrelated errand where pgbouncer statements were merely mentioned"}}` + "\n"
	if err := os.WriteFile(filepath.Join(hroot, "hm1.jsonl"), []byte(hbody), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := index.DefaultDir()
	if err := index.Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}

	ask := func(cwd string) string {
		var out bytes.Buffer
		payload := `{"prompt":"the pgbouncer prepared statements keep failing","cwd":"` + filepath.ToSlash(cwd) + `"}`
		t.Setenv("CLAUDE_PROJECT_DIR", "")
		if err := runHookPromptMode(dir, strings.NewReader(payload), &out, false); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	// The fixture has to answer from its own project, or silence from the home
	// directory would prove nothing.
	if got := ask(root); !strings.Contains(got, "pgbouncer") {
		t.Fatalf("the fixture recalls nothing even from its own project, so this test cannot fail honestly: %q", got)
	}
	if got := ask(sourcesHomeForTest()); strings.Contains(got, "deja-recall") {
		t.Errorf("the hook recalled from a home-directory scope: %q", got)
	}
}

func sourcesHomeForTest() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE")
}
