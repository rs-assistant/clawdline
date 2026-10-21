package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

func TestScheduleWebhookClientUsesMachineCredentialAndIdempotencyKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/schedule-webhooks/swh_test/activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer machine-secret" || r.Header.Get("Idempotency-Key") != "request-1" {
			t.Errorf("activation headers: %v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["expected_revision"] != float64(0) {
			t.Errorf("activation body: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": "clawdline.schedule_webhook.management.v1",
			"hook": map[string]any{"hook_id": "swh_test", "state": "active", "revision": 1}})
	})
	mux.HandleFunc("/v1/schedule-webhook-deliveries/claim", func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request["protocol"] != "clawdline.schedule_webhook.v2" {
			t.Errorf("claim protocol: %v", request)
		}
		if r.Header.Get("Authorization") != "Bearer machine-secret" {
			t.Errorf("claim authorization: %v", r.Header)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": schedulewebhook.ClaimSchema,
			"delivery": nil, "server_time": "2026-09-21T00:00:00Z", "poll_after_ms": 250})
	})
	mux.HandleFunc("/v1/schedule-webhook-deliveries/swd_test/receipts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "receipt:swd_test:1" {
			t.Errorf("receipt headers: %v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["task_id"]; !ok || body["task_id"] != nil {
			t.Errorf("receipt must carry task_id:null: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": "clawdline.schedule_webhook.receipt_ack.v1",
			"delivery_id": "swd_test", "receipt_version": 1, "state": "accepted",
			"acknowledged_at": "2026-09-21T00:00:00Z", "duplicate": false})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := ScheduleWebhookClient{Client: NewAccountClient(server.URL), Credential: "machine-secret"}
	if revision, err := client.Activate(context.Background(), "swh_test", "request-1", 0); err != nil || revision != 1 {
		t.Fatalf("activate: %d %v", revision, err)
	}
	if claim, err := client.Claim(context.Background(), 20); err != nil || claim.Delivery != nil || claim.PollAfter.Milliseconds() != 250 {
		t.Fatalf("claim: %+v %v", claim, err)
	}
	receipt := schedulewebhook.Receipt{Schema: schedulewebhook.ReceiptSchema, ReceiptVersion: 1,
		Kind: "mac_durable_accepted", OccurredAt: "2026-09-21T00:00:00Z", MacBuild: "test"}
	if ack, err := client.Receipt(context.Background(), "swd_test", receipt); err != nil || ack.ReceiptVersion != 1 {
		t.Fatalf("receipt: %+v %v", ack, err)
	}
}

func TestInvestigationBundleReadIsLeasedBoundedAndNeverRedirected(t *testing.T) {
	for _, mode := range []string{"ok", "too_large", "refused", "redirect", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/v1/schedule-webhook-deliveries/swd_fixture/investigation-bundle" || r.Header.Get("X-Clawdline-Lease") != "fixture-lease" || r.Header.Get("Authorization") != "Bearer fixture-machine" {
					t.Error("wrong machine lease transport")
				}
				switch mode {
				case "too_large":
					_, _ = w.Write([]byte(strings.Repeat(" ", schedulewebhook.MaxInvestigationBundleBytes+1)))
				case "refused":
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"error":{"code":"investigation_unavailable","message":"Unavailable"}}`))
				case "redirect":
					w.Header().Set("Location", "/credential-sink")
					w.WriteHeader(302)
				case "timeout":
					<-r.Context().Done()
				default:
					_, _ = w.Write([]byte(`{"schema":"fixture"}`))
				}
			}))
			defer server.Close()
			client := NewScheduleWebhookClient(server.URL, "fixture-machine")
			ctx := context.Background()
			if mode == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			raw, err := client.InvestigationBundle(ctx, schedulewebhook.Claim{DeliveryID: "swd_fixture", Lease: schedulewebhook.Lease{Token: "fixture-lease"}})
			if mode == "ok" {
				if err != nil || string(raw) != `{"schema":"fixture"}` {
					t.Fatalf("%s %v", raw, err)
				}
			} else if err == nil {
				t.Fatal("accepted failed/oversized/redirected read")
			}
			if calls != 1 {
				t.Fatalf("made %d requests, want exactly one", calls)
			}
		})
	}
}

func TestScheduleWebhookClientOutlivesTheProtocolLongPoll(t *testing.T) {
	client := NewScheduleWebhookClient("https://api.example.test", "credential")
	if client.Client.HTTP.Timeout <= 25*time.Second {
		t.Fatalf("webhook timeout %s cannot outlive a 25s server wait", client.Client.HTTP.Timeout)
	}
}
