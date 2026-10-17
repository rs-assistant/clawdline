package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// `clawdline board tracks`: the old cards on the three tracks of
// docs/board-redesign.md, by the same rules as `GET /v1/board/tracks`.
//
// It asks the running daemon. The cards were the retired Swift app's, whose
// board is no longer read (docs/cutover.md §7), so the replay of a board file
// that `--store` offered is gone with it.

func boardCommand(args []string) {
	if len(args) == 0 || args[0] != "tracks" {
		fmt.Fprintln(os.Stderr, cliCopy("workflow", "board.usage_clawdline_board_tracks_project_id_track_todo", "usage: clawdline board tracks [--project id] [--track todo|board|backlog] [--rows] [--json]"))
		os.Exit(2)
	}
	fs := flag.NewFlagSet("board tracks", flag.ExitOnError)
	project := fs.String("project", "", cliCopy("workflow", "board.only_this_project_s_cards", "only this project's cards"))
	track := fs.String("track", "", cliCopy("workflow", "board.list_the_rows_of_one_track_todo_board", "list the rows of one track: todo, board or backlog"))
	rows := fs.Bool("rows", false, cliCopy("workflow", "board.list_the_cards_under_their_buckets", "list the cards under their buckets"))
	asJSON := fs.Bool("json", false, cliCopy("workflow", "board.print_the_projection_as_json_every_row_included", "print the projection as JSON, every row included"))
	_ = fs.Parse(args[1:])
	if fs.NArg() > 0 {
		fail(fmt.Errorf(cliCopy("workflow", "board.unexpected_argument_q", "unexpected argument %q"), fs.Arg(0)))
	}
	switch work.Track(*track) {
	case "", work.TrackTodo, work.TrackBoard, work.TrackBacklog:
	default:
		fail(fmt.Errorf(cliCopy("workflow", "board.a_track_is_todo_board_or_backlog_not", "a track is todo, board or backlog, not %q"), *track))
	}

	t, err := daemonTracks(*project, work.Track(*track), *rows || *asJSON)
	if err != nil {
		fail(err)
	}
	if *asJSON {
		out, _ := json.MarshalIndent(map[string]any{"tracks": t}, "", "  ")
		fmt.Println(string(out))
		return
	}
	printTracks(os.Stdout, t, *rows)
}

// daemonTracks reads the route, every page of it when the rows are wanted.
func daemonTracks(project string, track work.Track, all bool) (work.Tracks, error) {
	var first work.Tracks
	cursor := ""
	for page := 0; ; page++ {
		// The board holds at most 2,000 cards (the Swift app's limit), ten
		// pages; a route that keeps handing out cursors past fifty is wrong.
		if page == 50 {
			return first, errors.New(cliCopy("workflow", "board.the_daemon_kept_paging_past_fifty_pages", "the daemon kept paging past fifty pages"))
		}
		q := url.Values{}
		if project != "" {
			q.Set("project", project)
		}
		if track != "" {
			q.Set("track", string(track))
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		path := "/v1/board/tracks"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		req, err := daemonRequest(http.MethodGet, path, nil)
		if err != nil {
			return first, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return first, err
		}
		if res.StatusCode != http.StatusOK {
			defer res.Body.Close()
			data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
			if refusal, ok := parseCLIHTTPRefusal(data); ok {
				return first, fmt.Errorf("%s: %s (%s)", res.Status, refusal.humanDetail(currentCLILanguage()), refusal.Code)
			}
			return first, errors.New(res.Status)
		}
		var body struct {
			Tracks work.Tracks `json:"tracks"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&body)
		res.Body.Close()
		if err != nil {
			return first, err
		}
		if page == 0 {
			first = body.Tracks
		} else {
			first.Rows = append(first.Rows, body.Tracks.Rows...)
		}
		if !all || body.Tracks.NextCursor == nil {
			first.NextCursor = nil
			return first, nil
		}
		cursor = *body.Tracks.NextCursor
	}
}

// printTracks is the §7.2 table, and the rows under it when asked.
func printTracks(w io.Writer, t work.Tracks, rows bool) {
	at := time.Unix(0, int64(t.EvaluatedAt*1e9)).UTC().Format(time.RFC3339)
	fmt.Fprintf(w, cliCopy("workflow", "board.rules_s_stall_s_evaluated_s", "rules     %s, stall %s, evaluated %s\n"), t.Rules, days(t.StallSeconds), at)
	if t.Project != nil {
		fmt.Fprintf(w, cliCopy("workflow", "board.project_s", "project   %s\n"), *t.Project)
	}
	b, h, p := t.Sources.Board, t.Sources.History, t.Sources.Presence
	fmt.Fprintf(w, cliCopy("workflow", "board.board_revision_d_d_cards_s", "board     revision %d, %d cards (%s)\n"), b.Revision, b.Cards, b.Status)
	fmt.Fprintf(w, cliCopy("workflow", "board.history_d_logs_d_unreadable_s", "history   %d logs, %d unreadable (%s)\n"), h.Files, h.Unreadable, h.Status)
	switch {
	case p.From == "none":
		fmt.Fprint(w, cliCopy("workflow", "board.presence_none_given_no_session_can_be_shown", "presence  none given: no session can be shown gone\n"))
	case p.Assumed:
		fmt.Fprintf(w, cliCopy("workflow", "board.presence_d_sessions_from_s_taken_as_complete", "presence  %d sessions from %s, taken as complete (--assume-complete; the reading said it was not)\n"), p.Sessions, p.From)
	case p.Complete:
		fmt.Fprintf(w, cliCopy("workflow", "board.presence_d_sessions_from_s_complete", "presence  %d sessions from %s, complete\n"), p.Sessions, p.From)
	default:
		fmt.Fprintf(w, cliCopy("workflow", "board.presence_d_sessions_from_s_incomplete_a_session", "presence  %d sessions from %s, incomplete: a session missing from it is unknown, not gone\n"), p.Sessions, p.From)
	}
	fmt.Fprintln(w)

	total := t.Counts.Cards
	byTrack := map[work.Track]int{work.TrackTodo: t.Counts.Todo, work.TrackBoard: t.Counts.Board,
		work.TrackBacklog: t.Counts.Backlog}
	for _, track := range work.TrackOrder {
		fmt.Fprintf(w, "%-24s %4d  %5s\n", track, byTrack[track], percent(byTrack[track], total))
		for _, bc := range t.Buckets {
			if bc.Track == track {
				fmt.Fprintf(w, "  %-22s %4d  %s\n", bc.Bucket, bc.Count, composition(bc.Progress))
			}
		}
	}
	fmt.Fprintf(w, "%-24s %4d\n\n", cliCopy("workflow", "board.cards", "cards"), total)

	count := map[work.Bucket]int{}
	for _, bc := range t.Buckets {
		count[bc.Bucket] = bc.Count
	}
	people := t.ForPeople()
	fmt.Fprintf(w, cliCopy("workflow", "board.for_people_d_backlog_d_s_board_closure", "for people %d: backlog %d (%s), board.closure %d (%s), board.now %d (%s), board.done %d (%s)\n"),
		people, t.Counts.Backlog, percent(t.Counts.Backlog, people),
		count[work.BoardClosure], percent(count[work.BoardClosure], people),
		count[work.BoardNow], percent(count[work.BoardNow], people),
		count[work.BoardDone], percent(count[work.BoardDone], people))

	if !rows {
		return
	}
	for _, bucket := range work.BucketOrder {
		listed := false
		for _, r := range t.Rows {
			if r.Bucket != bucket {
				continue
			}
			if !listed {
				fmt.Fprintf(w, "\n%s\n", bucket)
				listed = true
			}
			fmt.Fprintf(w, "  %-9s %-15s %-26s %s\n", r.Key, r.Facts.Progress, r.Rule, clip(r.Title, 70))
		}
	}
}

func percent(n, of int) string {
	if of == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(n)*100/float64(of))
}

func days(seconds float64) string {
	return strconv.FormatFloat(seconds/86400, 'g', -1, 64) + "d"
}

// composition is a bucket's progress states, largest first.
func composition(states map[string]int) string {
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if states[names[i]] != states[names[j]] {
			return states[names[i]] > states[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d", name, states[name]))
	}
	return strings.Join(parts, ", ")
}

func clip(s string, n int) string {
	runes := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n-1]) + "…"
}
