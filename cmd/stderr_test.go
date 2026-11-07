package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
)

// Under --error-format json the --debug trace is one {"debug":…} object per
// line; in text mode it is unchanged.
func TestDebugTraceUnderJSONErrorFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Setenv("C1I_DEBUG", "1")
			t.Setenv("C1I_ERROR_FORMAT", format)
			trace := captureStderr(t, func() {
				c := client.NewForTesting(srv.URL, srv.Client(), client.WithDebug(debugWriter()))
				if _, err := c.Get(context.Background(), "/api/v1/x", nil); err != nil {
					t.Fatal(err)
				}
			})
			lines := strings.Split(strings.TrimSpace(trace), "\n")
			if len(lines) != 2 {
				t.Fatalf("trace = %q, want two lines", trace)
			}
			for _, l := range lines {
				var obj map[string]string
				isObj := json.Unmarshal([]byte(l), &obj) == nil && len(obj) == 1 && obj["debug"] != ""
				if isObj != (format == "json") {
					t.Errorf("%s mode line %q", format, l)
				}
			}
		})
	}
	t.Run("off", func(t *testing.T) {
		t.Setenv("C1I_DEBUG", "")
		if debugWriter() != nil {
			t.Error("debugWriter is non-nil with --debug off")
		}
	})
}

// A notice keeps its text-mode line in text mode and becomes one object,
// without a "Warning: " prefix, in JSON mode.
func TestNoticeFormats(t *testing.T) {
	var b strings.Builder
	t.Setenv("C1I_ERROR_FORMAT", "text")
	noticeTo(&b, noticeURLDefault, "Warning: no --url flag given", map[string]any{"url": "u"})
	t.Setenv("C1I_ERROR_FORMAT", "json")
	noticeTo(&b, noticeURLDefault, "Warning: no --url flag given", map[string]any{"url": "u"})
	want := "Warning: no --url flag given\n" + `{"kind":"urlDefault","notice":"no --url flag given","url":"u"}` + "\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
