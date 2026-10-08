package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func withVersion(t *testing.T, v string) {
	t.Helper()
	saved := version
	version = v
	t.Cleanup(func() { version = saved })
}

// The line names both versions, the upgrade command for whatever installed the
// binary, and the variable that hides it; it says nothing when there is
// nothing newer (#4622).
func TestReleaseNoticeLine(t *testing.T) {
	line := releaseNoticeLine("1.0.0", "1.2.0", "/usr/local/bin/deja")
	for _, want := range []string{"v1.2.0", "v1.0.0", "`deja update`", releaseNoticeOff + "=1"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q does not contain %q", line, want)
		}
	}
	if got := releaseNoticeLine("1.0.0", "1.2.0", "/opt/homebrew/Cellar/deja-vu/1.0.0/bin/deja"); !strings.Contains(got, "`brew upgrade deja-vu`") {
		t.Errorf("a Homebrew binary is told %q", got)
	}
	for _, latest := range []string{"1.0.0", "0.9.0", ""} {
		if got := releaseNoticeLine("1.0.0", latest, "/usr/local/bin/deja"); got != "" {
			t.Errorf("latest %q: got %q, want nothing", latest, got)
		}
	}
}

func TestReleaseNoticeLooksOnceADayOnlyWhenInteractive(t *testing.T) {
	withVersion(t, "1.0.0")
	t.Setenv(releaseNoticeOff, "")
	now := time.Unix(1_800_000_000, 0)
	var looks atomic.Int32
	lookup := func() (string, bool) { looks.Add(1); return "1.2.0", true }
	stampIn := func(t *testing.T) string { return filepath.Join(t.TempDir(), "index.release") }

	if startReleaseNotice([]string{"search", "x"}, false, stampIn(t), now, lookup) != nil {
		t.Error("a hook, a pipe or the MCP server got a notice")
	}
	for _, cmd := range []string{"update", "doctor", "version", "--version"} {
		if startReleaseNotice([]string{cmd}, true, stampIn(t), now, lookup) != nil {
			t.Errorf("`deja %s` already speaks about versions, and got a notice too", cmd)
		}
	}
	t.Setenv(releaseNoticeOff, "1")
	if startReleaseNotice(nil, true, stampIn(t), now, lookup) != nil {
		t.Errorf("%s=1 did not hide the notice", releaseNoticeOff)
	}
	t.Setenv(releaseNoticeOff, "")

	stamp := stampIn(t)
	var out bytes.Buffer
	startReleaseNotice([]string{"search", "x"}, true, stamp, now, lookup).finish(&out, "/usr/local/bin/deja")
	if !strings.Contains(out.String(), "deja v1.2.0 is out") {
		t.Fatalf("first interactive run of the day said %q", out.String())
	}
	if startReleaseNotice([]string{"search", "x"}, true, stamp, now.Add(23*time.Hour), lookup) != nil {
		t.Error("a second look inside the day")
	}
	if startReleaseNotice([]string{"search", "x"}, true, stamp, now.Add(25*time.Hour), lookup) == nil {
		t.Error("no look the next day")
	}
	if b, _ := os.ReadFile(stamp); strings.TrimSpace(string(b)) != strconv.FormatInt(now.Add(25*time.Hour).Unix(), 10) {
		t.Errorf("stamp %q was not moved to the day's look", b)
	}
}

func TestReleaseNoticeSaysNothingForADevBuildOrASlowNetwork(t *testing.T) {
	t.Setenv(releaseNoticeOff, "")
	now := time.Unix(1_800_000_000, 0)
	withVersion(t, "dev")
	if startReleaseNotice(nil, true, filepath.Join(t.TempDir(), "s"), now, func() (string, bool) { return "9.9.9", true }) != nil {
		t.Error("a dev build was told it is behind")
	}

	withVersion(t, "1.0.0")
	release := make(chan struct{})
	defer close(release)
	slow := func() (string, bool) { <-release; return "1.2.0", true }
	var out bytes.Buffer
	started := time.Now()
	startReleaseNotice(nil, true, filepath.Join(t.TempDir(), "s"), now, slow).finish(&out, "/usr/local/bin/deja")
	if out.Len() != 0 {
		t.Errorf("a look that had not answered printed %q", out.String())
	}
	if waited := time.Since(started); waited > 2*releaseNoticeGrace {
		t.Errorf("the command waited %v for the network", waited)
	}
}
