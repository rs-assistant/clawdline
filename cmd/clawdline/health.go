package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// connectionHealthOffEnv turns the connection-health runtime off for a boot.
// Off is a supported state: the call sites receive a nil tracker and record
// nothing, and the local route answers `unsupported`.
const connectionHealthOffEnv = "CLAWDLINE_CONNECTION_HEALTH"

// startConnectionHealth makes the one runtime of this boot and runs its
// watchdog for the daemon's lifetime. A runtime that cannot be made (no
// random bytes for a boot id) leaves monitoring off and says so; it never
// stops the daemon.
func startConnectionHealth(ctx context.Context) *connectionhealth.Runtime {
	if strings.EqualFold(os.Getenv(connectionHealthOffEnv), "off") {
		log.Printf("connection health: off (%s=off)", connectionHealthOffEnv)
		return nil
	}
	build := buildRevision()
	if build == "" {
		build = version
	}
	rt, err := connectionhealth.NewRuntime(build, connectionhealth.DefaultCadence)
	if err != nil {
		log.Printf("connection health: off, the runtime could not be made: %v", err)
		return nil
	}
	go rt.Run(ctx)
	return rt
}

// healthTimeout bounds one CLI read. The route reads memory only, so a
// daemon that takes longer than this is itself the finding.
const healthTimeout = 10 * time.Second

// healthCommand is `clawdline health status|events|trace|export`: the
// daemon's connection-health watchdog and journal, read over loopback with
// this machine's own token. It never borrows a browser's credential and
// never asks Cloud.
func healthCommand(args []string) {
	cfg := config.Load()
	port := 0
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port, _ = strconv.Atoi(args[i+1])
		}
	}
	if port == 0 {
		p, err := daemonPort()
		if err != nil {
			fail(err)
		}
		port = p
	}
	client := &http.Client{Timeout: healthTimeout}
	newRequest := func(path string, q url.Values) (*http.Request, error) {
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		return daemonRequestWithConfig(http.MethodGet, path, nil, cfg, port)
	}
	os.Exit(healthRun(os.Stdout, os.Stderr, args, time.Now(), newRequest, client.Do))
}

const healthUsage = "usage: clawdline health <status|events|trace|export> [--since T] [--until T] [--after-seq N] [--limit N] [--boot-id ID] [--correlation ID] [--json] [--port n]\n" +
	"  T is RFC 3339 (2026-10-20T12:00:00Z) or a duration back from now (15m)"

// healthRun is the command without the process around it, for tests. It
// returns the exit status: 0 answered, 1 refused or unreachable, 2 usage.
func healthRun(stdout, stderr io.Writer, args []string, now time.Time,
	newRequest func(string, url.Values) (*http.Request, error),
	do func(*http.Request) (*http.Response, error)) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, healthUsage)
		return 2
	}
	sub := args[0]
	fs := flag.NewFlagSet("health "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	after := fs.String("after-seq", "", "")
	limit := fs.Int("limit", 0, "")
	boot := fs.String("boot-id", "", "")
	correlation := fs.String("correlation", "", "")
	asJSON := fs.Bool("json", false, "")
	_ = fs.Int("port", 0, "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, healthUsage)
		return 2
	}
	q := url.Values{}
	switch sub {
	case "status":
	case "events", "export":
		for _, f := range []struct{ name, value string }{{"since", *since}, {"until", *until}} {
			if f.value == "" {
				continue
			}
			t, err := healthTime(f.value, now)
			if err != nil {
				fmt.Fprintf(stderr, "clawdline health: --%s: %v\n", f.name, err)
				return 2
			}
			q.Set(f.name, t.UTC().Format(time.RFC3339Nano))
		}
		if *after != "" {
			// Checked here as well as by the daemon, so a cursor that does
			// not fit a sequence number is refused before any request.
			if _, err := strconv.ParseUint(*after, 10, 64); err != nil {
				fmt.Fprintln(stderr, "clawdline health: --after-seq is not a sequence number (invalid_cursor)")
				return 2
			}
			q.Set("after_seq", *after)
		}
		if *limit < 0 || *limit > connectionhealth.MaxQueryRecords {
			fmt.Fprintf(stderr, "clawdline health: --limit must be 0..%d (query_bound_exceeded)\n", connectionhealth.MaxQueryRecords)
			return 2
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		if *boot != "" {
			q.Set("boot_id", *boot)
		}
	case "trace":
		if !connectionhealth.ValidID(*correlation) {
			fmt.Fprintln(stderr, "clawdline health trace: --correlation must be an opaque id such as op-1k (invalid_field)")
			return 2
		}
		q.Set("correlation", *correlation)
	default:
		fmt.Fprintln(stderr, healthUsage)
		return 2
	}
	req, err := newRequest("/v1/connection-health/"+sub, q)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline health: %v\n", err)
		return 1
	}
	res, err := do(req)
	if err != nil {
		// Unreachable is not "healthy" and not "no events": it is unknown.
		fmt.Fprintf(stderr, "clawdline health: the daemon could not be asked; connection health is unknown: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, connectionhealth.MaxQueryBytes+connectionhealth.MaxJournalBytes))
	if err != nil {
		fmt.Fprintf(stderr, "clawdline health: the answer could not be read; connection health is unknown: %v\n", err)
		return 1
	}
	if res.StatusCode != http.StatusOK {
		// The daemon's one refusal shape: `error` is the code to branch on.
		var refusal struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(body, &refusal)
		if refusal.Error == "" {
			refusal.Error = strconv.Itoa(res.StatusCode)
		}
		fmt.Fprintf(stderr, "clawdline health %s: refused, %d %s: %s\n", sub, res.StatusCode, refusal.Error, strings.TrimSpace(refusal.Detail))
		return 1
	}
	if sub == "status" && !*asJSON {
		return healthSummary(stdout, stderr, body)
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") != nil {
		fmt.Fprintln(stderr, "clawdline health: the daemon's answer is not JSON; connection health is unknown")
		return 1
	}
	pretty.WriteByte('\n')
	_, _ = stdout.Write(pretty.Bytes())
	return 0
}

// healthTime reads an RFC 3339 instant or a duration back from now.
func healthTime(v string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return time.Time{}, errors.New("not an RFC 3339 time or a positive duration")
	}
	return now.Add(-d), nil
}

// healthSummary prints the status answer as a few lines: the quality first,
// so "could not see" is never read as "fine", then each finding that is not
// idle or progressing.
func healthSummary(stdout, stderr io.Writer, body []byte) int {
	var st connectionhealth.Status
	if json.Unmarshal(body, &st) != nil || st.Schema != connectionhealth.StatusSchema {
		fmt.Fprintln(stderr, "clawdline health: the daemon's answer is not a connection-health status; it is unknown")
		return 1
	}
	fmt.Fprintf(stdout, "boot %s  build %s  quality %s  running %t\n", st.BootID, st.Build, st.Quality, st.Running)
	if st.Report == nil {
		fmt.Fprintln(stdout, "watchdog: no scan yet (not_collected)")
		return 0
	}
	r := st.Report
	fmt.Fprintf(stdout, "watchdog: scan %d  trace coverage %s  transitions %s (lost %d)  overflow %d (critical %d)\n",
		r.ScanSeq, r.TraceCoverage, r.Transitions, r.TransitionsLost, r.Overflow.Total, r.Overflow.Critical)
	fmt.Fprintf(stdout, "journal: seq %d..%d  evicted %d  refused %d  %s\n",
		st.Coverage.FirstRetainedSeq, st.Coverage.LastSeq, st.Coverage.Evicted, st.Coverage.Refused, st.Coverage.Durability)
	shown := 0
	for _, f := range r.Findings {
		if f.State == connectionhealth.StateIdle || f.State == connectionhealth.StateProgressing {
			continue
		}
		shown++
		fmt.Fprintf(stdout, "  %-22s %-20s %-36s age %s  %s\n", f.State, f.Stage, f.Location,
			time.Duration(f.AgeNS).Round(time.Millisecond), f.Correlation)
	}
	if shown == 0 {
		fmt.Fprintf(stdout, "  %d active phases, none slow or blocked\n", len(r.Findings))
	}
	return 0
}
