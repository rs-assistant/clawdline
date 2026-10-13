package http

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

func TestNotificationRulesAreEditableAndNeverOverwriteAnInvalidFile(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "next")}}
	call := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/notification-rules", strings.NewReader(body))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		s.notificationRulesRoute(rec, req)
		return rec
	}
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	create := `{"action":"create","rule":{"id":"` + id + `","enabled":true,"after_minutes":3,"label_contains":"urgent"}}`
	if got := call(http.MethodPost, create); got.Code != 200 || !strings.Contains(got.Body.String(), `"after_minutes":3`) {
		t.Fatalf("create: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, create); got.Code != 200 {
		t.Fatalf("idempotent create: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"update","rule":{"id":"`+id+`","enabled":false,"after_minutes":5,"label_contains":"urgent"}}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"enabled":false`) {
		t.Fatalf("update: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"delete","id":"`+id+`"}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"rules":[]`) {
		t.Fatalf("delete: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"create","rule":{"id":"`+id+`","enabled":true,"after_minutes":121,"label_contains":""}}`); got.Code != 400 {
		t.Fatalf("invalid wait accepted: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"preview","text":"當 Session 等我回答時通知我"}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"after_minutes":0`) {
		t.Fatalf("immediate preview: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"preview","text":"當名稱包含「部署」的 Session 等我回答超過 5 分鐘時通知我"}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"label_contains":"部署"`) {
		t.Fatalf("filtered preview: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"preview","text":"當 Session 完成時通知我"}`); got.Code != 422 {
		t.Fatalf("unsupported preview was saved: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodGet, ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"rules":[]`) {
		t.Fatalf("read: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"preference","key":"push_on_waiting","value":false}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"push_on_waiting":false`) {
		t.Fatalf("preference: %d %s", got.Code, got.Body)
	}
	if got := call(http.MethodPost, `{"action":"preference","key":"hotkey","value":true}`); got.Code != 400 {
		t.Fatalf("native setting accepted: %d %s", got.Code, got.Body)
	}
}
