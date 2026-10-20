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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/adapters/healthupload"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// connectionHealthOffEnv turns the connection-health runtime off for a boot.
// Off is a supported state: the call sites receive a nil tracker and record
// nothing, and the local route answers `unsupported`.
const connectionHealthOffEnv = "CLAWDLINE_CONNECTION_HEALTH"

// connectionHealthStoreDir is the durable store's directory under the
// daemon's state directory.
func connectionHealthStoreDir(stateDir string) string {
	return filepath.Join(stateDir, "connection-health", "journal")
}

// startConnectionHealth makes the one runtime of this boot, hands it to the
// server, runs its watchdog for the daemon's lifetime, opens the durable
// store beside it and starts the health uploader. A runtime that cannot be
// made (no random bytes for a boot id) leaves monitoring off and says so; it
// never stops the daemon. The answer is nil when monitoring is off.
//
// The store is opened on its own goroutine: Open scans every segment (0.57 s
// on a full 96 MiB store, docs/admin-connection-health-storage.md §8) and a
// broken disk may make it slow or fail, and neither may hold the port or
// the first request. Until it opens the runtime is memory only and the
// route says `opening`; a failed open is reported with its code and the
// runtime stays memory only. Once open, the watchdog's one journal writer
// offers each record it appended, as stamped, to the store's non-blocking
// Offer (Watchdog.Attach).
//
// The uploader (internal/adapters/healthupload) runs on its own goroutine
// with its own HTTPS client. It makes no request unless the person opted in
// (`cloud_health_upload: true`), Cloud is on and this machine has an
// identity, and it reads only the durable store, never the Cloud line.
func startConnectionHealth(stateDir string, srv *httptransport.Server) *connectionHealthLife {
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
	ctx, cancel := context.WithCancel(context.Background())
	h := &connectionHealthLife{rt: rt, cancel: cancel,
		runDone: make(chan struct{}), opened: make(chan struct{}), uploadDone: make(chan struct{})}
	srv.SetConnectionHealth(rt)
	srv.SetConnectionHealthStore(nil, nil, true)
	go func() {
		defer close(h.runDone)
		rt.Run(ctx)
	}()
	go func() {
		defer close(h.opened)
		store, err := openConnectionHealthStore(rt, stateDir)
		if err == nil && ctx.Err() != nil {
			// The daemon began stopping while the store was opening: close
			// it here, or its lock and handles outlive the shutdown.
			closeCtx, done := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
			_ = store.Close(closeCtx)
			done()
			return
		}
		srv.SetConnectionHealthStore(store, err, false)
		if err != nil {
			log.Printf("connection health: the durable store could not be opened (%s); this boot is memory only", healthjournal.CodeOf(err))
			return
		}
		h.store.Store(store)
	}()
	h.upload = healthupload.New(healthupload.Options{
		StateDir: connectionHealthUploadDir(stateDir),
		Identity: healthUploadIdentity(stateDir, foreignDirs()),
		Source: func() healthupload.Source {
			if s := h.store.Load(); s != nil {
				return s
			}
			return nil
		},
		Log: func(format string, args ...any) { log.Printf(format, args...) },
	})
	srv.SetConnectionHealthUpload(h.upload.Status)
	go func() {
		defer close(h.uploadDone)
		h.upload.Run(ctx)
	}()
	return h
}

// connectionHealthStopTimeout bounds the whole connection-health shutdown:
// a stuck disk may cost this much of a stopping daemon, never more.
const connectionHealthStopTimeout = 3 * time.Second

// connectionHealthLife is the running monitor: its watchdog, its store once
// open, and its uploader.
type connectionHealthLife struct {
	rt         *connectionhealth.Runtime
	upload     *healthupload.Uploader
	cancel     context.CancelFunc
	store      atomic.Pointer[healthjournal.Store]
	runDone    chan struct{}
	opened     chan struct{}
	uploadDone chan struct{}
}

// Stop ends the producers first (the watchdog's scans, then its journal
// writer, which drains every transition already observed into the journal
// and the store's queue, and the uploader), then flushes and closes the
// store, all within ctx. A part that does not finish in time is named in
// the error and left behind — a held watchdog writer with how many observed
// transitions it had not yet journaled — and the store is still flushed and
// closed; Stop never waits past ctx and starts no goroutine of its own.
func (h *connectionHealthLife) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.cancel()
	var late []string
	for _, w := range []struct {
		name string
		done <-chan struct{}
	}{{"watchdog", h.runDone}, {"uploader", h.uploadDone}, {"store open", h.opened}} {
		select {
		case <-w.done:
		case <-ctx.Done():
			if w.done == h.runDone {
				queued, _ := h.rt.Watchdog.Pending()
				late = append(late, fmt.Sprintf("watchdog (writer held; %d observed transitions not journaled)", queued))
				continue
			}
			late = append(late, w.name)
		}
	}
	if s := h.store.Load(); s != nil {
		if _, err := s.Flush(ctx); err != nil {
			late = append(late, "store flush ("+string(healthjournal.CodeOf(err))+")")
		}
		if err := s.Close(ctx); err != nil {
			late = append(late, "store close ("+string(healthjournal.CodeOf(err))+")")
		}
	}
	if len(late) > 0 {
		return fmt.Errorf("connection health did not stop cleanly: %s", strings.Join(late, ", "))
	}
	return nil
}

// stopConnectionHealthOnSignal stops the monitor when the daemon is told to
// stop, then lets the signal do what it did before: the process ends the
// way the service manager expects.
func stopConnectionHealthOnSignal(h *connectionHealthLife) {
	if h == nil {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	sig := <-signals
	ctx, cancel := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
	if err := h.Stop(ctx); err != nil {
		log.Printf("%v", err)
	} else {
		log.Printf("connection health: stopped; the durable store is flushed and closed")
	}
	cancel()
	signal.Reset(os.Interrupt, syscall.SIGTERM)
	if p, err := os.FindProcess(os.Getpid()); err == nil && p.Signal(sig) == nil {
		// The default action ends the process; this wait is never reached
		// unless the signal is ignored by the platform.
		time.Sleep(time.Second)
	}
	os.Exit(1)
}

// healthUploadKey is the opt-in switch in the app's settings file. Anything
// but a JSON true is off, and off makes no request.
const healthUploadKey = "cloud_health_upload"

// connectionHealthUploadDir holds the uploader's progress file.
func connectionHealthUploadDir(stateDir string) string {
	return filepath.Join(stateDir, "connection-health", "upload")
}

// healthUploadIdentity reads, on every uploader cycle and from local files
// only, whether uploading is allowed and with which identity: the opt-in,
// Cloud's own switch, and the machine identity of the control plane the
// settings name. A mismatch between the two is refused, never resolved by
// picking one.
func healthUploadIdentity(stateDir string, foreign []string) healthupload.IdentityProvider {
	return func() (healthupload.Identity, error) {
		file := nextconfig.Open(stateDir, foreign...)
		values, err := file.Read()
		if err != nil {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeIdentityUnread}
		}
		var on bool
		if raw, ok := values.Raw[healthUploadKey]; !ok || json.Unmarshal(raw, &on) != nil || !on {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeNotOptedIn}
		}
		settings, err := adaptercloud.ReadSettings(file)
		if err != nil {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeIdentityUnread}
		}
		if !settings.Enabled {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeCloudOff}
		}
		identity, found, err := adaptercloud.NewIdentityStore(filepath.Join(stateDir, cloudkeys.DirName)).Load()
		switch {
		case err != nil:
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeIdentityUnread}
		case !found || !identity.Valid():
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeNoIdentity}
		}
		if adaptercloud.CheckEnvironment(identity, settings) != nil {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeOtherOrigin}
		}
		return healthupload.Identity{APIBase: settings.APIBase, Credential: identity.MachineCredential}, nil
	}
}

// openConnectionHealthStore opens the durable store for rt's boot and
// attaches it to rt's journal writer.
func openConnectionHealthStore(rt *connectionhealth.Runtime, stateDir string) (*healthjournal.Store, error) {
	dir, err := filepath.Abs(connectionHealthStoreDir(stateDir))
	if err != nil {
		return nil, err
	}
	store, err := healthjournal.Open(healthjournal.Options{Dir: filepath.Clean(dir), BootID: rt.BootID, Build: rt.Build})
	if err != nil {
		return nil, err
	}
	rt.Watchdog.Attach(func(r connectionhealth.Record) { _, _ = store.Offer(r) })
	return store, nil
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

const healthUsage = "usage: clawdline health <status|events|trace|export|history> [--since T] [--until T] [--after-seq N] [--limit N] [--boot-id ID] [--correlation ID] [--cursor C] [--json] [--port n]\n" +
	"  T is RFC 3339 (2026-10-20T12:00:00Z) or a duration back from now (15m)\n" +
	"  events/export read this boot's memory; history reads the durable store across boots (--cursor v1.<segment>.<offset>)"

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
	cursor := fs.String("cursor", "", "")
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
	case "history":
		if *limit < 0 || *limit > connectionhealth.MaxQueryRecords {
			fmt.Fprintf(stderr, "clawdline health: --limit must be 0..%d (query_bound_exceeded)\n", connectionhealth.MaxQueryRecords)
			return 2
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		if *cursor != "" {
			q.Set("cursor", *cursor)
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
	var durable struct {
		Durable *struct {
			State  string `json:"state"`
			Code   string `json:"code"`
			Status *struct {
				State   string `json:"state"`
				Records uint64 `json:"records"`
				Synced  uint64 `json:"synced_through"`
				Lost    uint64 `json:"lost_records"`
			} `json:"status"`
		} `json:"durable"`
		Upload *struct {
			State  string               `json:"state"`
			Status *healthupload.Status `json:"status"`
		} `json:"upload"`
	}
	_ = json.Unmarshal(body, &durable)
	if json.Unmarshal(body, &st) != nil || st.Schema != connectionhealth.StatusSchema {
		fmt.Fprintln(stderr, "clawdline health: the daemon's answer is not a connection-health status; it is unknown")
		return 1
	}
	fmt.Fprintf(stdout, "boot %s  build %s  quality %s  running %t\n", st.BootID, st.Build, st.Quality, st.Running)
	switch d := durable.Durable; {
	case d == nil:
		fmt.Fprintln(stdout, "durable store: unknown (not in this daemon's answer)")
	case d.Status != nil:
		fmt.Fprintf(stdout, "durable store: %s  %s  records %d  synced through ticket %d  lost %d\n",
			d.State, d.Status.State, d.Status.Records, d.Status.Synced, d.Status.Lost)
	case d.Code != "":
		fmt.Fprintf(stdout, "durable store: %s (%s); this boot is memory only\n", d.State, d.Code)
	default:
		fmt.Fprintf(stdout, "durable store: %s\n", d.State)
	}
	switch u := durable.Upload; {
	case u == nil:
		fmt.Fprintln(stdout, "cloud upload: unknown (not in this daemon's answer)")
	case u.Status == nil:
		fmt.Fprintf(stdout, "cloud upload: %s\n", u.State)
	default:
		t := u.Status.Totals
		pending := "none"
		if p := u.Status.Pending; p != nil {
			pending = fmt.Sprintf("%s (%d events, %d attempts, persisted %s)", p.BatchID, p.Events, p.Attempts, p.Persisted)
		}
		fmt.Fprintf(stdout, "cloud upload: %s  receipted %d  pending %s  refused %d  abandoned %d  this hour %d B / %d requests\n",
			u.Status.State, t.Receipted, pending, t.RefusedEvents, t.AbandonedEvents, u.Status.Hour.Bytes, u.Status.Hour.Requests)
	}
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
