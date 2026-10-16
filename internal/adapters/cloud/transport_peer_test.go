package cloud

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPeerFramesNeverEnterViewerEnvelopeCallback(t *testing.T) {
	var ingress, ack []byte
	var taskIngress, taskAck []byte
	var probeIngress, probeAck []byte
	transport := &Transport{opts: Options{
		PeerInbound:          func(frame []byte) { ingress = append([]byte(nil), frame...) },
		PeerAck:              func(frame []byte) { ack = append([]byte(nil), frame...) },
		PeerTaskStartInbound: func(frame []byte) { taskIngress = append([]byte(nil), frame...) },
		PeerTaskStartAck:     func(frame []byte) { taskAck = append([]byte(nil), frame...) },
		PeerTaskProbeInbound: func(frame []byte) { probeIngress = append([]byte(nil), frame...) },
		PeerTaskProbeAck:     func(frame []byte) { probeAck = append([]byte(nil), frame...) },
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
}
