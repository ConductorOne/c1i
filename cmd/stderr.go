package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Every stderr line c1i writes goes through this file or cmd/errors.go
// (TestStderrWritesOnlyThroughHelpers). Under --error-format json each line is
// one compact JSON object: {"error"}, {"notice"}, {"progress"} or {"debug"}.

// Notice kinds.
const (
	noticeURLDefault   = "urlDefault"
	noticeURLWarning   = "urlWarning"
	noticeResolved     = "resolved"
	noticeDryRun       = "dryRun"
	noticePlaceholders = "placeholders"
	noticeWaitDone     = "waitDone"
	noticeInfo         = "info"
)

// stderrIsJSON reports whether --error-format / C1I_ERROR_FORMAT is json.
func stderrIsJSON() bool {
	f, _ := errorFormatFlag.parse(viper.GetString("error_format"))
	return f == "json"
}

// notice writes one informational line to cmd's stderr. text is the
// text-mode line without its newline; JSON mode drops a "Warning: " prefix.
func notice(cmd *cobra.Command, kind, text string, fields map[string]any) {
	noticeTo(cmd.ErrOrStderr(), kind, text, fields)
}

// noticeTo is notice for callers with no command (warnAboutURL).
func noticeTo(w io.Writer, kind, text string, fields map[string]any) {
	if !stderrIsJSON() {
		_, _ = fmt.Fprintln(w, text)
		return
	}
	obj := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		obj[k] = v
	}
	obj["notice"] = strings.TrimPrefix(text, "Warning: ")
	obj["kind"] = kind
	writeJSONLine(w, obj)
}

// progress writes one --wait progress line.
func progress(w io.Writer, subject, text string, elapsed time.Duration) {
	if !stderrIsJSON() {
		_, _ = fmt.Fprintln(w, text)
		return
	}
	writeJSONLine(w, map[string]any{"progress": text, "subject": subject, "elapsed": elapsed.String()})
}

func writeJSONLine(w io.Writer, obj map[string]any) {
	b, err := json.Marshal(obj)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(w, string(b))
}

// debugWriter is the --debug trace sink every transport call site passes to
// WithDebug: nil when --debug is off, stderr in text mode, and a writer that
// wraps each line as {"debug":…} in JSON mode.
func debugWriter() io.Writer {
	if !viper.GetBool("debug") {
		return nil
	}
	if !stderrIsJSON() {
		return os.Stderr
	}
	return &jsonLineWriter{out: os.Stderr, key: "debug"}
}

// jsonLineWriter re-emits each complete line written to it as {key: line}.
type jsonLineWriter struct {
	mu  sync.Mutex
	out io.Writer
	key string
	buf []byte
}

func (j *jsonLineWriter) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.buf = append(j.buf, p...)
	for {
		i := bytes.IndexByte(j.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		writeJSONLine(j.out, map[string]any{j.key: string(j.buf[:i])})
		j.buf = j.buf[i+1:]
	}
}
