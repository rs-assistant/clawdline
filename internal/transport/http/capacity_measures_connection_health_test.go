package http

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthupload"
)

// cloud.health_upload_state_bytes is the progress file's size when the
// uploader knows it, and unmeasured — never a zero — before.
func TestTheUploadStateRowReadsTheProgressFileSize(t *testing.T) {
	now := time.Now()
	if r := uploadReading("cloud.health_upload_state_bytes", healthupload.Status{State: healthupload.CodeIdle}, now); r.Known {
		t.Fatalf("an unread progress file must be unmeasured: %+v", r)
	}
	n := 1234
	r := uploadReading("cloud.health_upload_state_bytes", healthupload.Status{State: healthupload.CodeIdle, StateBytes: &n}, now)
	if !r.Known || r.Used != 1234 {
		t.Fatalf("state_bytes 1234 read as %+v", r)
	}
}
