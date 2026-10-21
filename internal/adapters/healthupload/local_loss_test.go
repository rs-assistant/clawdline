package healthupload

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
)

// lossBody is a sent metadata-only batch as Cloud reads it.
type lossBody struct {
	BatchID   string            `json:"batch_id"`
	BootID    string            `json:"boot_id"`
	Events    []json.RawMessage `json:"events"`
	LocalLoss *struct {
		Schema       string            `json:"schema"`
		CounterEpoch string            `json:"counter_epoch"`
		ObservedAt   string            `json:"observed_at"`
		StorageLost  *uint64           `json:"storage_lost_records"`
		StoreGaps    map[string]uint64 `json:"store_gaps"`
		Excluded     map[string]uint64 `json:"excluded"`
	} `json:"local_loss"`
}

func lossBodies(t *testing.T, c *cloud) []lossBody {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []lossBody
	for _, raw := range c.bodies {
		var b lossBody
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatal(err)
		}
		if b.LocalLoss != nil {
			if b.Events == nil || len(b.Events) != 0 {
				t.Fatalf("local_loss rides only on a metadata-only batch with events []: %s", raw)
			}
			out = append(out, b)
		}
	}
	return out
}

// recreated leaves an uploader whose cursor named a store that was moved
// aside: one real invalid_cursor gap in its settled totals, and the
// records of the first store already receipted.
func recreated(t *testing.T) (*harness, *fixture, string) {
	t.Helper()
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-old")
	f.add(3)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.u.o.BootID = "boot-up"
	h.step(t)
	f.close()
	if err := os.Rename(filepath.Join(dir, "journal"), filepath.Join(dir, "journal.aside")); err != nil {
		t.Fatal(err)
	}
	f.open()
	h.now = h.now.Add(time.Minute)
	return h, f, dir
}

func TestStoreGapsReachCloudOnceAsLocalLossOfTheProgressFilesEpoch(t *testing.T) {
	h, _, _ := recreated(t)
	for i := 0; i < 4; i++ {
		h.step(t)
		h.now = h.now.Add(time.Minute)
	}
	sent := lossBodies(t, h.cloud)
	if len(sent) != 1 {
		t.Fatalf("one metadata-only batch per change of the totals, got %d", len(sent))
	}
	b := sent[0]
	epoch := h.u.Status().CounterEpoch
	if b.LocalLoss.Schema != LocalLossSchema || b.LocalLoss.CounterEpoch != epoch || !strings.HasPrefix(epoch, "ue-") ||
		b.BootID != "boot-up" || !strings.Contains(b.BatchID, strings.TrimPrefix(epoch, "ue-")) {
		t.Fatalf("the block must name the progress file's epoch and the uploader's boot: %+v epoch %q", b, epoch)
	}
	if b.LocalLoss.StoreGaps["invalid_cursor"] != 1 || b.LocalLoss.StorageLost == nil || *b.LocalLoss.StorageLost != 0 {
		t.Fatalf("the block must carry the settled gap exactly: %+v", b.LocalLoss)
	}
	if _, err := time.Parse(time.RFC3339, b.LocalLoss.ObservedAt); err != nil {
		t.Fatalf("observed_at is RFC 3339: %q", b.LocalLoss.ObservedAt)
	}
	if st := h.u.Status(); st.LastReceipt == nil || st.LastReceipt.BatchID != b.BatchID || st.LastReceipt.Accepted != 0 {
		t.Fatalf("the metadata-only batch is receipted with accepted 0: %+v", st.LastReceipt)
	}
}

func TestALostLocalLossAnswerIsResentByteForByte(t *testing.T) {
	h, _, _ := recreated(t)
	drop := true
	h.cloud.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if drop && strings.Contains(string(body), `"local_loss"`) {
			drop = false
			h.cloud.ingest(httptest.NewRecorder(), body)
			panic(http.ErrAbortHandler)
		}
		return false
	}
	for i := 0; i < 6; i++ {
		h.step(t)
		h.now = h.now.Add(time.Minute)
	}
	h.cloud.mu.Lock()
	var loss [][]byte
	for _, b := range h.cloud.bodies {
		if strings.Contains(string(b), `"local_loss"`) {
			loss = append(loss, b)
		}
	}
	h.cloud.mu.Unlock()
	if len(loss) != 2 || string(loss[0]) != string(loss[1]) {
		t.Fatalf("the retry must send the stored body, not a refreshed block: %d bodies", len(loss))
	}
	if st := h.u.Status(); st.LastReceipt == nil || !st.LastReceipt.Replay || st.Totals.RefusedBatches != 0 {
		t.Fatalf("the resent batch replays its receipt: %+v", st)
	}
}

func TestARestartKeepsTheEpochAndDoesNotReportTheSameTotalsAgain(t *testing.T) {
	h, f, dir := recreated(t)
	for i := 0; i < 3; i++ {
		h.step(t)
		h.now = h.now.Add(time.Minute)
	}
	epoch := h.u.Status().CounterEpoch
	before := h.u.Status().Totals.Gaps["invalid_cursor"]
	h.u = h.make()
	h.u.o.BootID = "boot-up2"
	for i := 0; i < 3; i++ {
		h.step(t)
		h.now = h.now.Add(time.Minute)
	}
	st := h.u.Status()
	if st.CounterEpoch != epoch || st.Totals.Gaps["invalid_cursor"] != before || len(lossBodies(t, h.cloud)) != 1 {
		t.Fatalf("a restart keeps the epoch, counts nothing twice and resends nothing: epoch %q→%q gaps %d→%d reports %d",
			epoch, st.CounterEpoch, before, st.Totals.Gaps["invalid_cursor"], len(lossBodies(t, h.cloud)))
	}
	// A progress file that is lost starts new totals under a new epoch,
	// so Cloud never reads the reset as a regression of the old one.
	if err := os.Remove(filepath.Join(dir, "upload", stateFile)); err != nil {
		t.Fatal(err)
	}
	f.add(1)
	h.u = h.make()
	h.u.o.BootID = "boot-up3"
	for i := 0; i < 3; i++ {
		h.step(t)
		h.now = h.now.Add(time.Minute)
	}
	if next := h.u.Status().CounterEpoch; next == "" || next == epoch {
		t.Fatalf("a new progress file mints a new epoch: %q after %q", next, epoch)
	}
	ids := map[string]bool{}
	for _, b := range lossBodies(t, h.cloud) {
		if ids[b.BatchID] {
			t.Fatalf("a new epoch's batch must not reuse batch_id %s", b.BatchID)
		}
		ids[b.BatchID] = true
	}
	if st := h.u.Status(); st.Totals.RefusedBatches != 0 {
		t.Fatalf("no batch of the new epoch may conflict with the old one: %+v", st.LastRefusal)
	}
}

func TestNoLocalLossIsSentWithoutLossOrWithoutABoot(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-clean")
	f.add(2)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.u.o.BootID = "boot-up"
	for i := 0; i < 3; i++ {
		h.step(t)
		h.now = h.now.Add(time.Minute)
	}
	if n := len(lossBodies(t, c)); n != 0 {
		t.Fatalf("nothing lost, nothing reported: %d", n)
	}
	h2, _, _ := recreated(t)
	h2.u.o.BootID = ""
	for i := 0; i < 3; i++ {
		h2.step(t)
		h2.now = h2.now.Add(time.Minute)
	}
	if n := len(lossBodies(t, h2.cloud)); n != 0 {
		t.Fatalf("without a boot id no metadata-only batch can be sent: %d", n)
	}
}

func TestTheLocalLossAllowlistsMatchTheGoReasons(t *testing.T) {
	for _, g := range []string{healthjournal.GapPruned, healthjournal.GapMissing, healthjournal.GapCorrupt, healthjournal.GapTorn, "invalid_cursor"} {
		if !cloudGapReasons[g] {
			t.Errorf("Go gap reason %q is not in Cloud's store_gaps allowlist", g)
		}
	}
	for _, r := range []string{ExcludedState, ExcludedEvent, ExcludedStage, ExcludedSource, ExcludedUnsafeInteger, ExcludedPattern, ExcludedTooLarge} {
		if !cloudExcludedReasons[r] {
			t.Errorf("Go exclusion %q is not in Cloud's excluded allowlist", r)
		}
	}
	// Cloud's bound is 16 keys per map; the allowlists are what can be sent.
	if len(cloudGapReasons) > 16 || len(cloudExcludedReasons) > 16 {
		t.Fatalf("an allowlist exceeds Cloud's 16 keys")
	}
}

func TestAnUnstatableTotalIsUnknownNotZero(t *testing.T) {
	at := t0
	if b := localLoss("ue-1", at, lossCounters{Gaps: map[string]uint64{"future_reason": 1}}); b != nil {
		t.Fatalf("a gap reason Cloud does not list cannot be sent, and leaving it out would read as 0: %+v", b)
	}
	if b := localLoss("ue-1", at, lossCounters{StorageLost: jsSafeInteger + 1}); b != nil {
		t.Fatalf("a value past the safe integer is not rounded: %+v", b)
	}
	if b := localLoss("", at, lossCounters{}); b != nil {
		t.Fatalf("no epoch, no block: %+v", b)
	}
	b := localLoss("ue-1", at, lossCounters{Excluded: map[string]uint64{"some_new_reason": 2, ExcludedTooLarge: 1}})
	if b == nil || b.Excluded[UnlistedReason] != 2 || b.Excluded[ExcludedTooLarge] != 1 || len(b.Excluded) != 2 {
		t.Fatalf("an exclusion reason Cloud does not list folds into unlisted: %+v", b)
	}
}
