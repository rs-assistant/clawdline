package cloud

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Exercise the public queue operations both with and without receipt history.
// The history must remain addressable without changing live delivery decisions.
func TestReceiptHistoryPreservesLiveDelivery(t *testing.T) {
	for _, receipts := range []int{0, 64} {
		t.Run(fmt.Sprint(receipts), func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			s := testSpool(t, &now)
			reserve := func(ch SpoolChannel, recipient string) uint64 {
				t.Helper()
				seq, err := s.Reserve(ch, recipient, "record", 10)
				if err != nil {
					t.Fatal(err)
				}
				return seq
			}
			for i := 0; i < receipts; i++ {
				seq := reserve(SpoolChannelT, "t/device/history")
				sealRow(t, s, seq, now)
				if d := s.SendNext(); d.Row == nil || d.Row.Seq != seq {
					t.Fatalf("history send: %+v", d)
				}
				kind := SettleDelivered
				if i%2 == 1 {
					kind = SettlePeerError
				}
				if _, err := s.Settle(seq, "t/device/history", kind); err != nil {
					t.Fatal(err)
				}
			}
			head := reserve(SpoolChannelCtlr, "ctlr/device/viewer")
			old := reserve(SpoolChannelS, "s/device/one")
			other := reserve(SpoolChannelS, "s/device/two")
			newer := reserve(SpoolChannelS, "s/device/one")
			for _, seq := range []uint64{old, other, newer} {
				sealRow(t, s, seq, now)
			}
			if d := s.SendNext(); !d.BlockedOnSeal || d.HeadSeq != head {
				t.Fatalf("reserved head: %+v", d)
			}
			if row, ok := s.Row(old); !ok || row.BurnReason != BurnReplacedByCoalesce {
				t.Fatalf("coalesced row: %+v", row)
			}
			sealRow(t, s, head, now)
			for _, seq := range []uint64{head, other, newer} {
				if d := s.SendNext(); d.Row == nil || d.Row.Seq != seq {
					t.Fatalf("global order want %d: %+v", seq, d)
				}
			}
			// A newer value must not remove the already-sent value.
			next, err := s.ReserveLatestValue(SpoolChannelS, "s/device/one", "record", 10)
			if err != nil {
				t.Fatal(err)
			}
			sealRow(t, s, next, now)
			if d := s.SendNext(); d.Row == nil || d.Row.Seq != next {
				t.Fatalf("next value: %+v", d)
			}
			if got := s.InFlight(); !reflect.DeepEqual(got, []uint64{head, other, newer, next}) {
				t.Fatalf("in flight: %v", got)
			}
			before, _ := s.Row(newer)
			ciphertext, err := s.Resend(newer)
			if err != nil || !bytes.Equal(ciphertext, before.Sealed) {
				t.Fatalf("resend: %q %v", ciphertext, err)
			}
			after, _ := s.Row(newer)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("resend changed the attempt")
			}
			if _, err := s.Settle(newer, "s/device/wrong", SettleDelivered); !errors.Is(err, ErrSpoolMismatch) {
				t.Fatalf("mismatch: %v", err)
			}
			if _, err := s.Settle(next+100, "s/device/one", SettleDelivered); !errors.Is(err, ErrSpoolRow) {
				t.Fatalf("unknown: %v", err)
			}
			if _, err := s.Settle(newer, "s/device/one", SettleDelivered); err != nil {
				t.Fatal(err)
			}
			if result, err := s.Settle(newer, "s/device/one", SettleDelivered); err != nil || result != SettleResultLateIgnored {
				t.Fatalf("duplicate: %s %v", result, err)
			}
			now = now.Add(31 * time.Second)
			if got := s.BurnExpired(); !reflect.DeepEqual(got, []uint64{head, other, next}) {
				t.Fatalf("expired: %v", got)
			}
			burned := s.TakeBurned()
			var seqs []uint64
			for _, row := range burned {
				seqs = append(seqs, row.Seq)
			}
			if !reflect.DeepEqual(seqs, []uint64{head, old, other, next}) {
				t.Fatalf("burn notifications: %v", seqs)
			}
			if len(s.TakeBurned()) != 0 {
				t.Fatal("burn notification repeated")
			}
			if result, err := s.Settle(head, "ctlr/device/viewer", SettleDelivered); err != nil || result != SettleResultLateIgnored {
				t.Fatalf("late burn ack: %s %v", result, err)
			}
			if !s.SendNext().Idle {
				t.Fatal("receipts became sendable")
			}
			if s.Rows() != receipts+5 {
				t.Fatalf("history lost: %d", s.Rows())
			}
		})
	}
}

func TestReceiptHistoryPreservesWindowAndExpiry(t *testing.T) {
	for _, byteWindow := range []bool{false, true} {
		t.Run(fmt.Sprint(byteWindow), func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			limits := DefaultSpoolLimits()
			limits.ReceiptCap = 2
			if byteWindow {
				limits.OutboundWindowByteCap = len(`{"ct":"x"}`)
			} else {
				limits.OutboundWindowRowCap = 1
			}
			s, err := NewSpool(limits, nil, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			reserve := func() uint64 {
				t.Helper()
				seq, err := s.Reserve(SpoolChannelT, "t/device/one", "read", 10)
				if err != nil {
					t.Fatal(err)
				}
				return seq
			}
			for i := 0; i < 3; i++ {
				seq := reserve()
				sealRow(t, s, seq, now)
				if d := s.SendNext(); d.Row == nil || d.Row.Seq != seq {
					t.Fatalf("send: %+v", d)
				}
				if _, err := s.Settle(seq, "t/device/one", SettleDelivered); err != nil {
					t.Fatal(err)
				}
				now = now.Add(time.Second)
			}
			first, second := reserve(), reserve()
			if _, err := s.Settle(0, "t/device/one", SettleDelivered); !errors.Is(err, ErrSpoolRow) {
				t.Fatalf("capped receipt: %v", err)
			}
			if got, err := s.Settle(1, "t/device/one", SettleDelivered); err != nil || got != SettleResultLateIgnored {
				t.Fatalf("retained receipt: %s %v", got, err)
			}
			_, reading := s.ChannelReadings()
			if reading.Used != 2 || reading.Counters.Expired != 1 {
				t.Fatalf("receipt capacity: %+v", reading)
			}
			sealRow(t, s, first, now)
			sealRow(t, s, second, now)
			if d := s.SendNext(); d.Row == nil || d.Row.Seq != first {
				t.Fatalf("window first: %+v", d)
			}
			if d := s.SendNext(); !d.BlockedOnAck || d.HeadSeq != first || d.Row != nil {
				t.Fatalf("window: %+v", d)
			}
			if _, err := s.Settle(first, "t/device/one", SettleDelivered); err != nil {
				t.Fatal(err)
			}
			if d := s.SendNext(); d.Row == nil || d.Row.Seq != second {
				t.Fatalf("window reopened: %+v", d)
			}
			staleReserved := reserve()
			now = now.Add(61 * time.Second)
			s.SendNext()
			if row, _ := s.Row(staleReserved); row.BurnReason != BurnStaleReservation {
				t.Fatalf("reservation: %+v", row)
			}
			staleReady := reserve()
			sealRow(t, s, staleReady, now)
			now = now.Add(241 * time.Second)
			s.SendNext()
			if row, _ := s.Row(staleReady); row.BurnReason != BurnStaleReadyAtSend {
				t.Fatalf("ready: %+v", row)
			}
			now = now.Add(limits.TombstoneRetention + time.Second)
			s.SendNext()
			if _, err := s.Settle(staleReady, "t/device/one", SettleDelivered); !errors.Is(err, ErrSpoolRow) {
				t.Fatalf("retention expired: %v", err)
			}
		})
	}
}
