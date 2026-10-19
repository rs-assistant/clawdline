package cloudops

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestHTMLDocumentPathAndAnswer(t *testing.T) {
	for _, path := range []string{"report.html", "sub/Report.HTML"} {
		if got, ok := documentPath(path); !ok || got != path {
			t.Fatalf("path: %q %v", got, ok)
		}
	}
	for _, path := range []string{"../report.html", ".hidden.html", "/report.html", "sub\\report.html", "report.xhtml", "report.html\x00"} {
		if _, ok := documentPath(path); ok {
			t.Errorf("accepted %q", path)
		}
	}
	for _, scope := range []string{"project", "task"} {
		body := []byte("<details><summary>Report</summary>safe</details>")
		answer, refusal := shapeDocument(plan{scope: scope, path: "report.html", task: "abcd"}, LocalResponse{ContentType: "text/html; charset=utf-8", Body: body})
		if refusal.Code != "" {
			t.Fatal(refusal)
		}
		var got map[string]any
		if err := json.Unmarshal(answer, &got); err != nil {
			t.Fatal(err)
		}
		if got["media_type"] != "text/html; charset=utf-8" || got["scope"] != scope || got["path"] != "report.html" || got["byte_count"] != float64(len(body)) || got["data"] != base64.StdEncoding.EncodeToString(body) {
			t.Fatal(got)
		}
		if scope == "task" && got["task"] != "abcd" {
			t.Fatal(got)
		}
		if scope == "project" && len(got) != 5 {
			t.Fatal(got)
		}
	}
}

func TestHTMLDocumentAnswerKeepsMIMEByteAndUTF8Checks(t *testing.T) {
	for _, row := range []struct {
		mime string
		body []byte
		code string
	}{
		{"text/html", []byte("safe"), "document_media_type_unsupported"},
		{"application/xhtml+xml", []byte("safe"), "document_media_type_unsupported"},
		{"text/html; charset=utf-8", []byte{0xff}, "document_not_utf8"},
		{"text/html; charset=utf-8", []byte(strings.Repeat("x", documentMaximumBytes+1)), "document_too_large"},
	} {
		if _, refusal := shapeDocument(plan{}, LocalResponse{ContentType: row.mime, Body: row.body}); refusal.Code != row.code {
			t.Fatalf("got %s; want %s", refusal.Code, row.code)
		}
	}
}
