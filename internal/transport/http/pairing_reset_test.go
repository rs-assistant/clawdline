package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// The review's finding: anybody who can reach the pairing route can begin a
// pairing of their own, type five wrong codes into it, and keep pairing closed
// for everybody for a day. The person at this machine — this machine's own
// token, and nobody else — can open it again; and five more wrong codes close
// it again, so the guessing budget is still five per person's say-so.
func TestThePersonAtTheMachineCanReopenPairingSomebodyElseClosed(t *testing.T) {
	g, h, local := openFixture(t)
	_, sender, err := g.auth.AddDevice("phone", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	asJSON := map[string]string{"Content-Type": "application/json"}
	begin := func() (int, contract.PairStarted) {
		rec := call{path: "/v1/auth/pair", body: `{"name":"x"}`, headers: asJSON}.do(h)
		var started contract.PairStarted
		_ = decode(rec.Body.Bytes(), &started)
		return rec.Code, started
	}
	closeIt := func() {
		t.Helper()
		status, started := begin()
		if status != http.StatusOK {
			t.Fatalf("the attacker's pairing: %d", status)
		}
		for i := 0; i < auth.PairingGuesses; i++ {
			call{path: "/v1/auth/pair/confirm", body: `{"pairing_id":"` + started.PairingID + `","code":"nope"}`, headers: asJSON}.do(h)
		}
		if status, _ := begin(); status != http.StatusTooManyRequests {
			t.Fatalf("after %d wrong codes, a pairing request: %d; want closed", auth.PairingGuesses, status)
		}
	}
	closeIt()

	reset := func(headers map[string]string) (int, contract.PairingReset) {
		rec := call{path: "/v1/auth/pairings/reset", body: `{}`, headers: headers}.do(h)
		var out contract.PairingReset
		_ = decode(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if status, _ := reset(asJSON); status != http.StatusUnauthorized {
		t.Fatalf("a reset with no credential: %d", status)
	}
	if status, _ := reset(map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + sender}); status != http.StatusForbidden {
		t.Fatalf("a reset by a paired device: %d", status)
	}
	if status, _ := reset(map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + local,
		actorHeader: actorDevice}); status != http.StatusForbidden {
		t.Fatalf("a reset by this machine's token judged as a device (a Cloud viewer): %d", status)
	}
	status, out := reset(map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + local})
	if status != http.StatusOK || out.WrongCodes != auth.PairingGuesses {
		t.Fatalf("the person's reset: %d %+v; want %d wrong codes forgotten", status, out, auth.PairingGuesses)
	}

	// The person's own pairing now goes through.
	notices, cancel := g.auth.Watch()
	defer cancel()
	status, started := begin()
	if status != http.StatusOK {
		t.Fatalf("a pairing request after the reset: %d", status)
	}
	var code string
	select {
	case n := <-notices:
		code = n.Code
	case <-time.After(2 * time.Second):
		t.Fatal("no code was shown")
	}
	rec := call{path: "/v1/auth/pair/confirm", body: `{"pairing_id":"` + started.PairingID + `","code":"` + code + `"}`, headers: asJSON}.do(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("the right code after the reset: %d %s", rec.Code, rec.Body)
	}

	// And the brute-force bound is intact.
	closeIt()
}

func decode(data []byte, v any) error { return json.Unmarshal(data, v) }
