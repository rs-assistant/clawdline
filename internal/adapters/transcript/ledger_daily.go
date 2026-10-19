package transcript

import (
	"crypto/sha256"
	"fmt"
	"os"
	"time"
)

// DailyDayLimit includes today and two complete 28-day comparison windows.
const DailyDayLimit = 57

// FeedDaily upgrades a cumulative-only cursor without rewinding its bill.
// A separate bounded Feed backfills dates until both cursors reach the same
// end. At most two ordinary Feed budgets are used during the migration.
// The replay holds primitives only, just like the original ledger state.
func (s *LedgerState) FeedDaily(path string, now time.Time) (FeedResult, error) {
	return s.feedDaily(path, now, ledgerFeedLimit, ledgerLineLimit)
}

func (s *LedgerState) feedDaily(path string, now time.Time, feedLimit, lineLimit int64) (FeedResult, error) {
	if s.DailyVersion != 1 || (s.DailyTimezone != "" && s.DailyTimezone != DailyTimezoneKey(now)) {
		if s.Offset == 0 {
			s.PrepareDaily(now)
		} else {
			s.DailyReplay = &LedgerState{Assistant: s.Assistant}
			s.DailyVersion = 1
			s.DailyTimezone = DailyTimezoneKey(now)
		}
	}
	replay := s.DailyReplay
	if replay == nil {
		s.PrepareDaily(now)
		return s.feed(path, feedLimit, lineLimit)
	}
	res, err := s.feed(path, feedLimit, lineLimit)
	if err != nil {
		return res, err
	}
	if res.Restarted {
		replay = &LedgerState{Assistant: s.Assistant}
	}
	replay.PrepareDaily(now)
	backfill, err := replay.feed(path, feedLimit, lineLimit)
	if err != nil {
		return res, err
	}
	s.DailyVersion, s.DailyTimezone = 1, replay.DailyTimezone
	s.Daily, s.DailyProject = replay.Daily, replay.DailyProject
	s.DailyCutoff, s.DailyUnknown = replay.DailyCutoff, replay.DailyUnknown
	s.DailyUntil = replay.DailyUntil
	same := false
	if !res.More && !backfill.More && s.Offset == replay.Offset {
		same, err = sameDailySource(path, s, replay)
		if err != nil {
			return res, err
		}
	}
	if same {
		s.DailyReplay = nil
	} else {
		s.DailyReplay = replay
		res.More = true
	}
	return res, nil
}

// A legacy cursor may have fingerprinted a shorter initial file. Compare
// both prefix lengths against the source rather than comparing unlike hashes.
func sameDailySource(path string, a, b *LedgerState) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, recordError(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	head, err := fingerprint(f, st.Size())
	if err != nil {
		return false, err
	}
	matches := func(state *LedgerState) bool {
		if state.HeadLen == 0 {
			return st.Size() == 0 && state.HeadSum == ""
		}
		return digest(head, state.HeadLen) == state.HeadSum
	}
	return matches(a) && matches(b), nil
}

// PrepareDaily forces a replay of legacy cumulative-only states. The caller
// supplies its clock; provider timestamps, never file mtimes, name the days.
func (s *LedgerState) PrepareDaily(now time.Time) {
	cutoff := now.In(time.Local).AddDate(0, 0, -(DailyDayLimit - 1)).Format("2006-01-02")
	if s.DailyVersion != 1 || (s.DailyTimezone != "" && s.DailyTimezone != DailyTimezoneKey(now)) {
		*s = LedgerState{Assistant: s.Assistant}
	}
	s.DailyVersion, s.DailyCutoff = 1, cutoff
	s.DailyTimezone = DailyTimezoneKey(now)
	s.DailyUntil = now.In(time.Local).AddDate(0, 0, 1).Format("2006-01-02")
	for date := range s.Daily {
		if date < cutoff {
			delete(s.Daily, date)
		}
	}
}

func (s *LedgerState) dailyMeasured() Tokens {
	t := s.Measured
	if s.Open != nil {
		t.Output += float64(s.Open.Output)
	}
	if s.OpenSide != nil {
		t.Output += float64(s.OpenSide.Output)
	}
	return t
}

func (s *LedgerState) dailyRecord(rec object, before Tokens) {
	if s.DailyReplay != nil {
		return
	}
	if s.DailyProject == "" {
		s.DailyProject, _ = rec.str("cwd")
		if kind, _ := rec.str("type"); kind == "session_meta" {
			if payload, ok := rec.object("payload"); ok {
				s.DailyProject, _ = payload.str("cwd")
			}
		}
	}
	after := s.dailyMeasured()
	delta := Tokens{Input: after.Input - before.Input, CacheRead: after.CacheRead - before.CacheRead,
		CacheWrite1h: after.CacheWrite1h - before.CacheWrite1h, CacheWrite5m: after.CacheWrite5m - before.CacheWrite5m,
		Output: after.Output - before.Output}
	if delta.Total() == 0 {
		return
	}
	stamp, _ := rec.str("timestamp")
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		s.DailyUnknown = true
		return
	}
	date := at.In(time.Local).Format("2006-01-02")
	if s.DailyUntil != "" && date >= s.DailyUntil {
		s.DailyUnknown = true
		return
	}
	if s.DailyVersion != 1 || s.DailyCutoff == "" {
		s.PrepareDailyWindow(at)
	}
	if date < s.DailyCutoff {
		return
	}
	target := &s.Daily
	if *target == nil {
		*target = map[string]Tokens{}
	}
	t := (*target)[date]
	t.add(delta)
	(*target)[date] = t
}

// PrepareDailyWindow bounds direct parser use as well as the background pass.
func (s *LedgerState) PrepareDailyWindow(at time.Time) {
	cutoff := at.In(time.Local).AddDate(0, 0, -(DailyDayLimit - 1)).Format("2006-01-02")
	if cutoff > s.DailyCutoff {
		s.DailyCutoff = cutoff
	}
	for date := range s.Daily {
		if date < s.DailyCutoff {
			delete(s.Daily, date)
		}
	}
}

// DailyTimezoneKey identifies the transition rules covering retained history.
// Local can keep its name when the operating system's timezone changes. Walk
// zone transitions, rather than only the present UTC offset (which changes at
// DST), over the previous and current calendar years. The key stays stable
// during each year and changes when any relevant offset rule changes.
func DailyTimezoneKey(now time.Time) string {
	year := now.In(time.Local).Year()
	begin := time.Date(year-1, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	hash := sha256.New()
	fmt.Fprintf(hash, "%s/%d", time.Local.String(), year)
	for at := begin; at.Before(end); {
		local := at.In(time.Local)
		name, offset := local.Zone()
		_, next := local.ZoneBounds()
		fmt.Fprintf(hash, "/%s:%d:%d", name, offset, next.Unix())
		if next.IsZero() || !next.After(at) {
			break
		}
		at = next
	}
	return fmt.Sprintf("%s:%x", time.Local.String(), hash.Sum(nil))
}
