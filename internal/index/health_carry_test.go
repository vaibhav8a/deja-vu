package index

import (
	"os"
	"path/filepath"
	"testing"
)

// The health map is how doctor answers "what could deja not read" — and the
// answer has to outlive an unrelated pass, because the lines it counts are
// still on disk. The merge path built a fresh manifest and carried five fields
// forward but not this one, so the whole map went at the next pass that took it
// (#2015).
func TestTheHealthMapSurvivesAnUnrelatedPass(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("DEJA_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("DEJA_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("DEJA_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	t.Setenv("DEJA_NOTES_FILE", filepath.Join(tmp, "notes.jsonl"))
	proj := filepath.Join(tmp, "claude", "-tmp-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	turn := func(id, ts, role, text string) string {
		return `{"type":"` + role + `","sessionId":"` + id + `","cwd":"/tmp/app","timestamp":"` + ts +
			`","message":{"role":"` + role + `","content":"` + text + `"}}` + "\n"
	}
	// The second line carries a raw escape, which makes it invalid JSON.
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(
		turn("s1", "2026-01-02T03:04:05Z", "user", "why does pgbouncer time out")+
			turn("s1", "2026-01-02T03:04:06Z", "user", "the pool \x1b[31mtimed out")), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The premise: the build that read the bad line recorded it. Without this
	// the assertions below would hold on a store that never lost anything.
	if got := m.IngestHealth["claude"].MalformedLines; got != 1 {
		t.Fatalf("the build recorded %d unreadable lines, so there is nothing to carry", got)
	}

	// A new session in the same store — a change that does not touch the file
	// holding the bad line. This is the merge path.
	if err := os.WriteFile(filepath.Join(proj, "s2.jsonl"), []byte(
		turn("s2", "2026-02-02T03:04:05Z", "user", "unrelated question about retries")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	m, err = readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Sessions) != 2 {
		t.Fatalf("the pass indexed %d sessions, so it was not the merge path this is about", len(m.Sessions))
	}
	if got := m.IngestHealth["claude"].MalformedLines; got != 1 {
		t.Errorf("the unreadable line is still on disk and the count is %d: doctor has forgotten it", got)
	}

	// The other half: a file rewritten without its bad line clears its own
	// count, or carrying counts forward would make them permanent.
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(
		turn("s1", "2026-01-02T03:04:05Z", "user", "why does pgbouncer time out")+
			turn("s1", "2026-01-02T03:04:06Z", "user", "the pool timed out")+
			turn("s1", "2026-01-02T03:04:07Z", "assistant", "raised it to 40")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	m, err = readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.IngestHealth["claude"].MalformedLines; got != 0 {
		t.Errorf("the bad line is gone from the file and the count is still %d", got)
	}
}
