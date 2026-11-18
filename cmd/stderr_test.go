package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
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

// Server text in a text-mode error reaches stderr with control characters
// escaped, and JSON mode keeps its bytes.
func TestTextErrorEscapesServerControlChars(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	body := "line1\n\x1b]0;pwned\x07\x1b[2J\u202e\u2028\u0085tail\tend"
	srv := newMutServer(t, fixedReply(400, body))

	t.Setenv("C1I_ERROR_FORMAT", "text")
	res := runMutationCLI(t, srv.srv, 0, nil, "apps", "get", "app-1", "--error-format", "text")
	for _, raw := range []string{"\x1b", "\x07", "\u202e", "\u2028", "\u0085"} {
		if strings.Contains(res.stderr, raw) {
			t.Errorf("stderr holds raw %q: %q", raw, res.stderr)
		}
	}
	for _, want := range []string{`\x1b]0;pwned\x07\x1b[2J`, "\\u202e", "\\u2028", `\x85tail`, "line1\n", "tail\tend"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr lacks %q: %q", want, res.stderr)
		}
	}

	res = runMutationCLI(t, srv.srv, 0, nil, "apps", "get", "app-1", "--error-format", "json")
	obj := res.errorObject(t)
	if !strings.Contains(obj["error"].(string), "\x1b]0;pwned") {
		t.Errorf("json error lost the raw text: %v", obj["error"])
	}
	if strings.ContainsAny(res.stderr, "\x1b\x07") {
		t.Errorf("json stderr holds a raw control byte: %q", res.stderr)
	}
}

func TestEscapeControl(t *testing.T) {
	for in, want := range map[string]string{
		"plain\n\té":   "plain\n\té",
		"\x00\r\x7f":   `\x00\x0d\x7f`,
		"\u2066\u2069": `\u2066\u2069`,
		"\x9b":         `\x9b`,
		"\xc0\x9b":     `\xc0\x9b`,
		"a\xe2\x80":    `a\xe2\x80`,
		"\xed\xa0\x80": `\xed\xa0\x80`,
		"\x1b\x9b":     `\x1b\x9b`,
		"\u202a\u202d": `\u202a\u202d`,
	} {
		if got := escapeControl(in); got != want {
			t.Errorf("escapeControl(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoticeAndProgressEscapeInTextMode(t *testing.T) {
	t.Setenv("C1I_ERROR_FORMAT", "text")
	var b strings.Builder
	noticeTo(&b, noticeInfo, "n\x1b[2J", nil)
	progress(&b, "s", "p\x1b[2J", 0)
	if got, want := b.String(), "n\\x1b[2J\np\\x1b[2J\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A control-char status line reaches the --debug trace escaped.
func TestDebugTraceEscapesServerStatusLine(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, "HTTP/1.1 400 Bad\x1b[2J\x07 Req\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
			_ = c.Close()
		}
	}()
	t.Setenv("C1I_DEBUG", "1")
	t.Setenv("C1I_ERROR_FORMAT", "text")
	t.Setenv("C1I_URL", mutTestURL)
	var res mutResult
	trace := captureStderr(t, func() {
		res = runCLI(t, func() {
			orig := newClient
			newClient = func(*cobra.Command, string) (*client.Client, error) {
				return client.NewForTesting("http://"+ln.Addr().String(), http.DefaultClient,
					client.WithMaxRetries(0), client.WithDebug(debugWriter())), nil
			}
			t.Cleanup(func() { newClient = orig })
		}, nil, "apps", "get", "app-1")
	})
	if !strings.Contains(trace, `Bad\x1b[2J\x07 Req`) {
		t.Errorf("trace lacks the escaped status: %q (err %v)", trace, res.err)
	}
	if strings.ContainsAny(trace+res.stderr, "\x1b\x07") {
		t.Errorf("raw control byte on stderr: %q %q", trace, res.stderr)
	}
}

// A multi-id batch failure prints each id's error escaped.
func TestBatchItemErrorEscapesServerControlChars(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	t.Setenv("C1I_ERROR_FORMAT", "text")
	srv := newMutServer(t, fixedReply(400, "bad\x1b[2Jbody"))
	res := runMutationCLI(t, srv.srv, 0, nil, "mcp", "tools", "approve", "t1", "t2", "--app-id", "app-x", "--connector-id", "conn-y")
	if !strings.Contains(res.stderr, `bad\x1b[2Jbody`) {
		t.Errorf("no escaped item error: %q", res.stderr)
	}
	if strings.Contains(res.stderr, "\x1b") {
		t.Errorf("raw ESC on stderr: %q", res.stderr)
	}
}
