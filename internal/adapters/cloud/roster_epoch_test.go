package cloud

import (
	"encoding/json"
	"testing"
)

func TestMachineRosterDistinguishesUnknownAndKnownZeroEpoch(t *testing.T) {
	for _, tc := range []struct {
		body  string
		known bool
		epoch int
	}{
		{`{"id":"machine-a"}`, false, 0},
		{`{"id":"machine-a","identity_epoch":null}`, false, 0},
		{`{"id":"machine-a","identity_epoch":0}`, true, 0},
		{`{"id":"machine-a","identity_epoch":7}`, true, 7},
	} {
		var row MachineRecord
		if err := json.Unmarshal([]byte(tc.body), &row); err != nil {
			t.Fatal(err)
		}
		if (row.IdentityEpoch != nil) != tc.known {
			t.Fatalf("known = %v, want %v", row.IdentityEpoch != nil, tc.known)
		}
		if tc.known && *row.IdentityEpoch != tc.epoch {
			t.Fatalf("epoch = %d, want %d", *row.IdentityEpoch, tc.epoch)
		}
	}
}
