package healthsealed

import (
	"errors"
	"testing"
)

// Owner-sealed diagnostics go to Cloud over https. An http:// API base is
// refused even on a loopback host unless a test asks for it, so a settings
// file that names one cannot send the sealed body and its credential in clear
// to whatever listens on that port.
func TestTheSealedUploaderRefusesAnHTTPAPIBase(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:9", "http://localhost:9", "http://[::1]:9", "http://api.example.test"} {
		_, err := (&Uploader{Endpoint: endpoint}).target()
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeBadInput {
			t.Fatalf("%s: %v", endpoint, err)
		}
	}
	if got, err := (&Uploader{Endpoint: "https://api.example.test/"}).target(); err != nil || got != "https://api.example.test"+UploadPath {
		t.Fatalf("https: %q %v", got, err)
	}
	if got, err := (&Uploader{Endpoint: "http://127.0.0.1:9", AllowLoopbackHTTP: true}).target(); err != nil || got != "http://127.0.0.1:9"+UploadPath {
		t.Fatalf("loopback for a test: %q %v", got, err)
	}
	if _, err := (&Uploader{Endpoint: "http://api.example.test", AllowLoopbackHTTP: true}).target(); err == nil {
		t.Fatal("AllowLoopbackHTTP admitted a host that is not loopback")
	}
}
