package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// releaseNoticeOff is the variable that silences the once-a-day line (#4622).
const releaseNoticeOff = "DEJA_NO_UPDATE_NOTICE"

// releaseNoticeInterval is how long one look at the latest release stands.
const releaseNoticeInterval = 24 * time.Hour

// releaseNoticeGrace is how long a finished command waits for a check still in
// flight. A slow network costs this once a day at most, and the line is then
// simply not shown.
const releaseNoticeGrace = 300 * time.Millisecond

// releaseNoticeSkips are the commands that already speak about versions, or
// whose whole job is the upgrade: a second line there would only repeat them.
var releaseNoticeSkips = map[string]bool{
	"update": true, "doctor": true, "version": true, "--version": true, "-v": true,
}

// releaseNotice is a look at the latest release, started before the command
// runs and read after it, so the network is never on the command's own path.
type releaseNotice struct {
	done    chan string
	current string
}

// startReleaseNotice begins the once-a-day look when this run may show its
// answer, and returns nil otherwise. Only an interactive run qualifies: both
// stdout and stderr a terminal, which a hook or the MCP server never has.
// The stamp is written before the look, so a failed or offline look still
// waits a day instead of retrying on every command.
func startReleaseNotice(args []string, interactive bool, stamp string, now time.Time, lookup doctorVersionLookup) *releaseNotice {
	if !interactive || os.Getenv(releaseNoticeOff) != "" {
		return nil
	}
	if len(args) > 0 && releaseNoticeSkips[args[0]] {
		return nil
	}
	current := normalizeUpdateVersion(version)
	if _, ok := parseUpdateVersion(current); !ok {
		return nil // a dev build has nothing to be behind
	}
	if b, err := os.ReadFile(stamp); err == nil {
		if ts, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && now.Sub(time.Unix(ts, 0)) < releaseNoticeInterval {
			return nil
		}
	}
	if err := os.WriteFile(stamp, []byte(strconv.FormatInt(now.Unix(), 10)), 0o600); err != nil {
		return nil
	}
	n := &releaseNotice{done: make(chan string, 1), current: current}
	go func() {
		latest, _ := lookup()
		n.done <- latest
	}()
	return n
}

// finish writes the line when a newer release came back within the grace.
func (n *releaseNotice) finish(w io.Writer, exe string) {
	if n == nil {
		return
	}
	select {
	case latest := <-n.done:
		if line := releaseNoticeLine(n.current, latest, exe); line != "" {
			fmt.Fprintln(w, line)
		}
	case <-time.After(releaseNoticeGrace):
	}
}

// releaseNoticeLine is the one line, with the upgrade command for whatever
// installed this binary, or "" when there is nothing newer to say.
func releaseNoticeLine(current, latest, exe string) string {
	if order, ok := compareUpdateVersions(current, latest); !ok || order >= 0 {
		return ""
	}
	command := "deja update"
	if _, managed := packageManagerOwning(exe); managed != "" {
		command = managed
	}
	return fmt.Sprintf("deja v%s is out (this is v%s) — `%s` upgrades it; %s=1 hides this line", latest, current, command, releaseNoticeOff)
}
