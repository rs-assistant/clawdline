package cloud

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"

	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPeerFramesNeverEnterViewerEnvelopeCallback(t *testing.T) {
	var ingress, ack []byte
	var taskIngress, taskAck []byte
	var probeIngress, probeAck []byte
	var eventIngress, eventAck []byte
	transport := &Transport{opts: Options{
		PeerInbound:          func(frame []byte) { ingress = append([]byte(nil), frame...) },
		PeerAck:              func(frame []byte) { ack = append([]byte(nil), frame...) },
		PeerTaskStartInbound: func(frame []byte) { taskIngress = append([]byte(nil), frame...) },
		PeerTaskStartAck:     func(frame []byte) { taskAck = append([]byte(nil), frame...) },
		PeerTaskProbeInbound: func(frame []byte) { probeIngress = append([]byte(nil), frame...) },
		PeerTaskProbeAck:     func(frame []byte) { probeAck = append([]byte(nil), frame...) },
		PeerTaskEventInbound: func(frame []byte) { eventIngress = append([]byte(nil), frame...) },
		PeerTaskEventAck:     func(frame []byte) { eventAck = append([]byte(nil), frame...) },
		Inbound: func(_ domaincloud.Envelope, _ []byte, _ ed25519.PublicKey) {
			t.Fatal("machine peer traffic entered the viewer envelope lane")
		},
	}}
	// The callback itself has no trust authority. It only selects a separate
	// queue where the locally pinned key and grant are later checked.
	envelope := []byte(`{"type":"peer_envelope","request":{}}`)
	if err := transport.handle(nil, envelope); err != nil || !bytes.Equal(ingress, envelope) {
		t.Fatalf("peer ingress = %q, %v", ingress, err)
	}
	receipt := []byte(`{"type":"peer_ack","request_id":"id","status":"delivered"}`)
	if err := transport.handle(nil, receipt); err != nil || !bytes.Equal(ack, receipt) {
		t.Fatalf("peer ack = %q, %v", ack, err)
	}
	task := []byte(`{"type":"peer_task_start_envelope","start":{}}`)
	if err := transport.handle(nil, task); err != nil || !bytes.Equal(taskIngress, task) {
		t.Fatalf("task ingress = %q, %v", taskIngress, err)
	}
	taskReceipt := []byte(`{"type":"peer_task_start_ack","request_id":"id","status":"delivered"}`)
	if err := transport.handle(nil, taskReceipt); err != nil || !bytes.Equal(taskAck, taskReceipt) {
		t.Fatalf("task ack = %q, %v", taskAck, err)
	}
	probe := []byte(`{"type":"peer_task_probe_envelope","probe":{}}`)
	if err := transport.handle(nil, probe); err != nil || !bytes.Equal(probeIngress, probe) {
		t.Fatalf("probe ingress = %q, %v", probeIngress, err)
	}
	probeReceipt := []byte(`{"type":"peer_task_probe_ack","status":"delivered"}`)
	if err := transport.handle(nil, probeReceipt); err != nil || !bytes.Equal(probeAck, probeReceipt) {
		t.Fatalf("probe ack = %q, %v", probeAck, err)
	}
	event := []byte(`{"type":"peer_task_event_envelope","event":{}}`)
	if err := transport.handle(nil, event); err != nil || !bytes.Equal(eventIngress, event) {
		t.Fatalf("event ingress = %q, %v", eventIngress, err)
	}
	eventReceipt := []byte(`{"type":"peer_task_event_ack","status":"delivered"}`)
	if err := transport.handle(nil, eventReceipt); err != nil || !bytes.Equal(eventAck, eventReceipt) {
		t.Fatalf("event ack = %q, %v", eventAck, err)
	}
}

// TestEveryKnownPeerFrameIsHandledSilently keeps each peer frame type the
// transport knows from also being reported as unknown. A case that forgets
// its return falls out of the switch into the "ignored" line, so the frame is
// delivered and a misleading log line is written for every one of them.
func TestEveryKnownPeerFrameIsHandledSilently(t *testing.T) {
	var lines []string
	transport := &Transport{opts: Options{
		Log:                  func(line string) { lines = append(lines, line) },
		PeerInbound:          func([]byte) {},
		PeerAck:              func([]byte) {},
		PeerDiscoveryStatus:  func([]byte) {},
		PeerDiscoveryInbound: func([]byte) {},
		PeerTaskStartInbound: func([]byte) {},
		PeerTaskStartAck:     func([]byte) {},
		PeerTaskProbeInbound: func([]byte) {},
		PeerTaskProbeAck:     func([]byte) {},
		PeerTaskEventInbound: func([]byte) {},
		PeerTaskEventAck:     func([]byte) {},
	}}
	for _, kind := range []string{"peer_envelope", "peer_ack", "peer_discovery_ack", "peer_discovery_error",
		"peer_discovery_envelope", "peer_task_start_envelope", "peer_task_start_ack",
		"peer_task_probe_envelope", "peer_task_probe_ack", "peer_task_event_envelope", "peer_task_event_ack"} {
		lines = nil
		if err := transport.handle(nil, []byte(`{"type":"`+kind+`"}`)); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		for _, line := range lines {
			if strings.Contains(line, "unknown frame") {
				t.Errorf("%s was delivered and also logged %q", kind, line)
			}
		}
	}
}
