package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/redact"
	"github.com/spf13/cobra"
)

// This file is the only place that sends a mutating request, runs a mutating
// command's pre-send read or --wait, or writes mutation output
// (TestMutationsWriteOnlyThroughHelper). The contract: stdout is the API's
// response, unwrapped like the family's read and never projected; --dry-run
// prints {"dryRun":true,"requests":[…]}; every error carries `applied`.

const mutatingAnnotation = "c1i/mutating"

// markMutating tags c so writeError reports `applied` for its errors, including
// ones raised before the helper runs.
func markMutating(c *cobra.Command) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[mutatingAnnotation] = "true"
}

func isMutating(c *cobra.Command) bool {
	return c != nil && c.Annotations[mutatingAnnotation] == "true"
}

// unwrapRule is how a response becomes the printed document; the zero value is
// byID("id"). It must match the family's read.
type unwrapRule struct {
	idKey, named string
	raw          bool
}

func byID(key string) unwrapRule  { return unwrapRule{idKey: key} }
func named(key string) unwrapRule { return unwrapRule{named: key} }
func raw() unwrapRule             { return unwrapRule{raw: true} }

func (u unwrapRule) apply(data []byte) []byte {
	switch {
	case u.raw:
		return data
	case u.named != "":
		return unwrapNamedEnvelope(data, u.named)
	case u.idKey != "":
		return unwrapEnvelope(data, u.idKey)
	default:
		return unwrapEnvelope(data, "id")
	}
}

// mutation is one write request.
type mutation struct {
	Method  string     // never GET
	Path    string     // built with client.Path
	Body    any        // untyped nil = no body
	RawBody []byte     // api only; sent verbatim
	Unwrap  unwrapRule // zero = byID("id")
}

// resolvedID is an id a pre-send read looked up on the caller's behalf. It is
// reported on stderr only: "Resolved <Label>: <ID>".
type resolvedID struct {
	Name  string // body key, e.g. "policyStepId"
	Label string // e.g. "policy step"
	ID    string
}

// preRead is a read the helper runs before the send. It may modify m.
type preRead struct {
	Run      func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error)
	OnDryRun bool
}

type secretKind int

const (
	secretNone secretKind = iota
	secretSPCredential
	secretAutomationWebhook
)

type mutationOpts struct {
	preReads    []preRead
	wait        func(c *client.Client) waitOp[[]string]
	waitEnabled bool
	secret      secretKind
	batchVerb   string
	batchNoun   string
}

type mutationOpt func(*mutationOpts)

func withPreRead(p preRead) mutationOpt {
	return func(o *mutationOpts) { o.preReads = append(o.preReads, p) }
}

// withWait polls build's op after a successful write when enabled.
func withWait(build func(c *client.Client) waitOp[[]string], enabled bool) mutationOpt {
	return func(o *mutationOpts) { o.wait, o.waitEnabled = build, enabled }
}

// secretBearing adds the R22 wording to a failed stdout write.
func secretBearing(kind secretKind) mutationOpt {
	return func(o *mutationOpts) { o.secret = kind }
}

// withBatchWording names a batch's text-mode lines: "<noun> <id>: <err>" and
// "<verb> N of M <noun>s; K failed: …".
func withBatchWording(verb, noun string) mutationOpt {
	return func(o *mutationOpts) { o.batchVerb, o.batchNoun = verb, noun }
}

func collectMutationOpts(opts []mutationOpt) mutationOpts {
	o := mutationOpts{batchVerb: "applied", batchNoun: "request"}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// sendMutation runs one write: refuse --fields, resolve the URL, pre-send
// reads, guards, then preview or send, print, and --wait.
func sendMutation(cmd *cobra.Command, m mutation, opts ...mutationOpt) error {
	o := collectMutationOpts(opts)
	if err := refuseMutationFields(cmd); err != nil {
		return notApplied(err)
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return notApplied(err)
	}
	c, err := runPreReads(cmd, baseURL, &m, o)
	if err != nil {
		return notApplied(err)
	}
	req, err := prepareMutation(baseURL, m)
	if err != nil {
		return notApplied(err)
	}
	if dryRunActive() {
		return notApplied(writeMutationPreview(cmd, baseURL, []previewRequest{req}))
	}
	if c == nil {
		if c, err = newMutationClient(cmd, baseURL); err != nil {
			return notApplied(err)
		}
	}
	data, err := c.Request(cmd.Context(), m.Method, m.Path, req.Body, nil)
	if err != nil {
		return &mutationError{err: fmt.Errorf("API error: %w", err), applied: classifyApplied(err)}
	}
	if err := writeMutationResult(cmd, m, data, o.secret, false); err != nil {
		return err
	}
	if o.waitEnabled && o.wait != nil {
		op := o.wait(c)
		if _, err := runWait(cmd, op); err != nil {
			return &mutationError{
				err:     fmt.Errorf("%s %s was applied; waiting for %s failed: %w", m.Method, m.Path, op.Subject, err),
				applied: appliedTrue,
			}
		}
	}
	return nil
}

// mutationStep is one request of a sequence. Build receives the ids earlier
// steps returned (their Placeholder on a dry run).
type mutationStep struct {
	Build       func(ids []string) mutation
	Placeholder string                            // stand-in for this step's id in a preview; "" for the last step
	ResultID    func(data []byte) (string, error) // nil for the last step
	WrapErr     func(err error, ids []string) error
}

type stepResult struct {
	Method  string       `json:"method"`
	Path    string       `json:"path"`
	Applied appliedState `json:"applied"`
}

// sendMutationSequence sends steps in order and prints only the last
// response. note explains the preview's placeholders on stderr.
func sendMutationSequence(cmd *cobra.Command, steps []mutationStep, note string, opts ...mutationOpt) error {
	o := collectMutationOpts(opts)
	if err := refuseMutationFields(cmd); err != nil {
		return notApplied(err)
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return notApplied(err)
	}
	if dryRunActive() {
		ids := make([]string, 0, len(steps))
		var reqs []previewRequest
		var placeholders []string
		for _, s := range steps {
			m := s.Build(ids)
			req, err := prepareMutation(baseURL, m)
			if err != nil {
				return notApplied(err)
			}
			reqs = append(reqs, req)
			ids = append(ids, s.Placeholder)
			if s.Placeholder != "" {
				placeholders = append(placeholders, s.Placeholder)
			}
		}
		if err := writeMutationPreview(cmd, baseURL, reqs); err != nil {
			return notApplied(err)
		}
		if len(placeholders) > 0 && note != "" {
			notice(cmd, noticePlaceholders, note, map[string]any{"placeholders": placeholders})
		}
		return nil
	}
	c, err := runPreReads(cmd, baseURL, nil, o)
	if err != nil {
		return notApplied(err)
	}
	if c == nil {
		if c, err = newMutationClient(cmd, baseURL); err != nil {
			return notApplied(err)
		}
	}
	ids := make([]string, 0, len(steps))
	var done []stepResult
	fail := func(s mutationStep, err error, applied appliedState) error {
		if s.WrapErr != nil {
			err = s.WrapErr(err, ids)
		}
		for _, d := range done {
			if d.Applied == appliedTrue {
				applied = appliedTrue
			}
		}
		return &mutationError{err: err, applied: applied, steps: done}
	}
	for i, s := range steps {
		m := s.Build(ids)
		req, err := prepareMutation(baseURL, m)
		if err != nil {
			return fail(s, err, appliedFalse)
		}
		data, err := c.Request(cmd.Context(), m.Method, m.Path, req.Body, nil)
		if err != nil {
			a := classifyApplied(err)
			done = append(done, stepResult{m.Method, m.Path, a})
			return fail(s, fmt.Errorf("API error: %w", err), a)
		}
		done = append(done, stepResult{m.Method, m.Path, appliedTrue})
		if i == len(steps)-1 {
			if err := writeMutationResult(cmd, m, data, o.secret, false); err != nil {
				var me *mutationError
				if errors.As(err, &me) {
					me.steps = done
				}
				return err
			}
			return nil
		}
		id, err := s.ResultID(data)
		if err != nil {
			return fail(s, err, appliedTrue)
		}
		ids = append(ids, id)
	}
	return nil
}

// sendMutationBatch sends one mutation per addressed id (label(i)) and prints
// one compact line per applied id. idKey is the per-line id path.
func sendMutationBatch(cmd *cobra.Command, ms []mutation, idKey string, label func(int) string, opts ...mutationOpt) error {
	o := collectMutationOpts(opts)
	_ = idKey // asserted by TestBatchLinesCarryAddressedID
	labels := make([]string, len(ms))
	for i := range ms {
		labels[i] = label(i)
	}
	if err := refuseMutationFields(cmd); err != nil {
		return notApplied(err)
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return notApplied(err)
	}
	// R17: any failure before the first send reports the offending id and
	// lists every id as not attempted, with that failure's own exit code.
	unsent := func(i int, err error) error {
		writeBatchItemError(cmd, o.batchNoun, labels[i], err, appliedFalse, len(ms) > 1)
		return &mutationError{err: err, applied: appliedFalse, exit: exitCode(err), batch: &batchSummary{NotAttempted: labels}}
	}
	var c *client.Client
	reqs := make([]previewRequest, len(ms))
	for i := range ms {
		if c, err = runPreReadsWith(cmd, baseURL, &ms[i], o, c); err != nil {
			return unsent(i, err)
		}
		if reqs[i], err = prepareMutation(baseURL, ms[i]); err != nil {
			return unsent(i, err)
		}
	}
	if dryRunActive() {
		return notApplied(writeMutationPreview(cmd, baseURL, reqs))
	}
	if c == nil {
		if c, err = newMutationClient(cmd, baseURL); err != nil {
			return notApplied(err)
		}
	}

	var (
		firstErr  error
		firstCode int
		failed    int
		succeeded int
		anyTrue   bool
		anyUnk    bool
	)
	record := func(i int, err error, a appliedState) {
		failed++
		if firstErr == nil {
			firstErr, firstCode = err, exitCode(err)
		}
		anyTrue = anyTrue || a == appliedTrue
		anyUnk = anyUnk || a == appliedUnknown
		writeBatchItemError(cmd, o.batchNoun, labels[i], err, a, len(ms) > 1)
	}
	summary := func(msg string, code int, notAttempted []string) error {
		a := appliedFalse
		switch {
		case anyTrue || succeeded > 0:
			a = appliedTrue
		case anyUnk:
			a = appliedUnknown
		}
		if notAttempted == nil {
			notAttempted = []string{}
		}
		return &mutationError{
			err:     errors.New(msg),
			applied: a,
			exit:    code,
			batch:   &batchSummary{Succeeded: succeeded, Failed: failed, NotAttempted: notAttempted},
		}
	}
	total := len(ms)
	head := func() string {
		return fmt.Sprintf("%s %d of %d %ss; %d failed", o.batchVerb, succeeded, total, o.batchNoun, failed)
	}
	for i, m := range ms {
		if cmd.Context().Err() != nil {
			return summary(head()+": "+cmd.Context().Err().Error(), exitCode(cmd.Context().Err()), labels[i:])
		}
		data, err := c.Request(cmd.Context(), m.Method, m.Path, reqs[i].Body, nil)
		if err != nil {
			record(i, fmt.Errorf("API error: %w", err), classifyApplied(err))
			if cmd.Context().Err() != nil {
				return summary(head()+": "+firstErr.Error(), firstCode, labels[i+1:])
			}
			continue
		}
		if err := writeMutationResult(cmd, m, data, o.secret, true); err != nil {
			var nj *nonJSONResponseError
			if errors.As(err, &nj) {
				record(i, err, appliedTrue)
				continue
			}
			record(i, err, appliedTrue)
			return summary(head()+"; output failed: "+err.Error(), exitError, labels[i+1:])
		}
		succeeded++
	}
	if failed == 0 {
		return nil
	}
	if total == 1 && !stderrIsJSON() {
		return summary(firstErr.Error(), firstCode, nil)
	}
	return summary(head()+": "+firstErr.Error(), firstCode, nil)
}

func newMutationClient(cmd *cobra.Command, baseURL string) (*client.Client, error) {
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	return c, nil
}

func runPreReads(cmd *cobra.Command, baseURL string, m *mutation, o mutationOpts) (*client.Client, error) {
	return runPreReadsWith(cmd, baseURL, m, o, nil)
}

// runPreReadsWith runs o's pre-send reads that apply to this run, building a
// client (reusing c) only when one does.
func runPreReadsWith(cmd *cobra.Command, baseURL string, m *mutation, o mutationOpts, c *client.Client) (*client.Client, error) {
	dry := dryRunActive()
	for _, p := range o.preReads {
		if dry && !p.OnDryRun {
			continue
		}
		if c == nil {
			var err error
			if c, err = newMutationClient(cmd, baseURL); err != nil {
				return nil, err
			}
		}
		if m == nil {
			m = &mutation{}
		}
		resolved, err := p.Run(cmd.Context(), c, m)
		if err != nil {
			return nil, err
		}
		for _, r := range resolved {
			notice(cmd, noticeResolved, fmt.Sprintf("Resolved %s: %s", r.Label, r.ID),
				map[string]any{"name": r.Name, "id": r.ID})
		}
	}
	return c, nil
}

// refuseMutationFields: mutation output is never projected, so an explicit
// --fields (even "") is a usage error. A valid C1I_FIELDS alone is ignored
// (a malformed one already failed in validateGlobalSettings).
func refuseMutationFields(cmd *cobra.Command) error {
	if f := cmd.Flags().Lookup("fields"); f != nil && f.Changed {
		return &usageError{errors.New("--fields does not apply to mutation output, which is always the full response; drop --fields and extract with jq")}
	}
	return nil
}

// previewRequest is one request as it would be sent.
type previewRequest struct {
	Method string          `json:"method"`
	URL    string          `json:"url"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// prepareMutation marshals m's body once and applies the send-time guards
// (G1 as a usage error, G2 as the client's *PathError).
func prepareMutation(baseURL string, m mutation) (previewRequest, error) {
	body, err := marshalMutationBody(m)
	if err != nil {
		return previewRequest{}, err
	}
	if err := checkMutationRequest(m.Method, baseURL+m.Path); err != nil {
		return previewRequest{}, err
	}
	return previewRequest{Method: m.Method, URL: baseURL + m.Path, Body: body}, nil
}

func checkMutationRequest(method, rawURL string) error {
	err := client.CheckRequest(method, rawURL)
	var pe *client.PathError
	if err != nil && !errors.As(err, &pe) {
		redact.ErrorURL(err)
		return &usageError{fmt.Errorf("building %s %s: %w", method, redact.URL(rawURL), err)}
	}
	return err
}

// marshalMutationBody returns the bytes to send, nil for no body. Only an
// untyped-nil Body with no RawBody means no body; a typed-nil map or slice is
// sent as {} or [], and anything else marshaling to null is refused.
func marshalMutationBody(m mutation) ([]byte, error) {
	if m.RawBody != nil {
		return m.RawBody, nil
	}
	if m.Body == nil {
		return nil, nil
	}
	b, err := json.Marshal(m.Body)
	if err != nil {
		return nil, fmt.Errorf("encoding request body: %w", err)
	}
	if string(b) == "null" {
		switch reflect.ValueOf(m.Body).Kind() {
		case reflect.Map:
			return []byte("{}"), nil
		case reflect.Slice:
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("refusing to send %s %s: the request body encodes to null", m.Method, m.Path)
	}
	return b, nil
}

// writeMutationPreview prints the --dry-run document and its stderr notice.
func writeMutationPreview(cmd *cobra.Command, baseURL string, reqs []previewRequest) error {
	_ = baseURL // reqs carry the full URL
	reqs = redactPreviewBodies(reqs)
	doc := struct {
		DryRun   bool             `json:"dryRun"`
		Requests []previewRequest `json:"requests"`
	}{true, reqs}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // bodies are shown as sent, apart from secrets
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encoding dry-run preview: %w", err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, bytes.TrimSpace(buf.Bytes()), "", "  "); err != nil {
		return fmt.Errorf("encoding dry-run preview: %w", err)
	}
	pretty.WriteByte('\n')
	if _, err := cmd.OutOrStdout().Write(pretty.Bytes()); err != nil {
		return wrapOutputErr(err)
	}
	notice(cmd, noticeDryRun, "dry run: nothing was sent", nil)
	return nil
}

// redactPreviewBodies is the one place a preview masks secrets: every preview
// goes through writeMutationPreview. A body with no secret key keeps its bytes.
func redactPreviewBodies(reqs []previewRequest) []previewRequest {
	out := make([]previewRequest, len(reqs))
	copy(out, reqs)
	for i, r := range out {
		out[i].URL = redact.URL(r.URL)
		if len(r.Body) == 0 {
			continue
		}
		if !json.Valid(r.Body) {
			out[i].Body = json.RawMessage(`"[redacted: not JSON]"`) // cannot be inspected, so not shown
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(r.Body))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil {
			continue
		}
		red := redactSecrets(v)
		if reflect.DeepEqual(v, red) {
			continue
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if enc.Encode(red) == nil {
			out[i].Body = bytes.TrimSpace(buf.Bytes())
		}
	}
	return out
}

// previewProbeIfDryRun previews a probe POST (which is not a mutation, so it
// does not go through sendMutation) instead of sending it, and reports whether
// it did. Secret-valued keys in the body are masked: a probe's body carries
// the credentials under test.
func previewProbeIfDryRun(cmd *cobra.Command, baseURL, path string, body map[string]any) (bool, error) {
	if !dryRunActive() {
		return false, nil
	}
	req, err := prepareMutation(baseURL, mutation{Method: http.MethodPost, Path: path, Body: body})
	if err != nil {
		return true, err
	}
	return true, writeMutationPreview(cmd, baseURL, []previewRequest{req})
}

// maskValue reports whether the value under key k (folded sk) is a credential.
// An object under an auth-arm key or a "credential(s)" key is a container, not
// a payload: it is recursed into so its own credential fields are masked.
func maskValue(k, sk string, v any) bool {
	container := authArmKeys[sk] || sk == "credential" || sk == "credentials"
	if isScalar(v) {
		return authArmKeys[sk] || redact.IsSecretKey(k)
	}
	return redact.IsSecretKey(k) && !container
}

// redactSecrets returns a copy of v with the value of every secret key masked,
// whatever its JSON type; an empty value is left as is.
func redactSecrets(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			sk := redact.FoldKey(k)
			if !emptyJSONValue(e) && maskValue(k, sk, e) {
				out[k] = redact.Placeholder
			} else if strings.Contains(sk, "headers") {
				out[k] = redactHeaders(e)
			} else if str, ok := e.(string); ok {
				out[k] = redact.URL(str)
			} else {
				out[k] = redactSecrets(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			if str, ok := e.(string); ok {
				out[i] = redact.URL(str)
			} else {
				out[i] = redactSecrets(e)
			}
		}
		return out
	}
	return v
}

// authArmKeys are the auth-oneof arm names (authTemplateFlag's api names); a
// string placed directly under one is a credential.
var authArmKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, v := range authTemplateFlag.values {
		m[redact.FoldKey(v.api)] = true
	}
	return m
}()

// redactHeaders masks every non-empty header value, as a {name: value} map or
// a [{name, value}] list: any header can carry a credential (Authorization,
// X-Api-Key, ...). The name fields stay.
func redactHeaders(v any) any {
	switch h := v.(type) {
	case string: // "Authorization: Bearer ..."
		return maskUnlessEmpty(h)
	case map[string]any:
		out := make(map[string]any, len(h))
		for k, e := range h {
			out[k] = maskUnlessEmpty(e)
		}
		return out
	case []any:
		out := make([]any, len(h))
		for i, e := range h {
			m, ok := e.(map[string]any)
			if !ok {
				out[i] = maskUnlessEmpty(e)
				continue
			}
			row := make(map[string]any, len(m))
			for k, f := range m {
				if n := redact.FoldKey(k); n == "name" || n == "key" || n == "headername" {
					row[k] = f
				} else {
					row[k] = maskUnlessEmpty(f)
				}
			}
			out[i] = row
		}
		return out
	}
	return v
}

func maskUnlessEmpty(v any) any {
	if emptyJSONValue(v) {
		return v
	}
	return redact.Placeholder
}

// isScalar reports a JSON value that is neither an object nor an array.
func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

func emptyJSONValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}

// writeMutationResult prints U(data): pretty for a single target, compact for
// a batch line. Failures here are after the change was applied.
func writeMutationResult(cmd *cobra.Command, m mutation, data []byte, secret secretKind, compact bool) error {
	doc := bytes.TrimSpace(data)
	if len(doc) == 0 {
		doc = []byte("{}")
	}
	if !json.Valid(doc) {
		return &mutationError{
			err: &nonJSONResponseError{fmt.Errorf("%s %s returned 2xx with a non-JSON body; the change may have been applied: %w",
				m.Method, m.Path, jsonSyntaxErr(doc))},
			applied: appliedTrue,
		}
	}
	doc = m.Unwrap.apply(doc)
	var out bytes.Buffer
	var err error
	if compact {
		err = json.Compact(&out, doc)
	} else {
		err = json.Indent(&out, doc, "", "  ")
	}
	if err != nil {
		return &mutationError{err: &nonJSONResponseError{fmt.Errorf("%s %s returned 2xx with a non-JSON body; the change may have been applied: %w", m.Method, m.Path, err)}, applied: appliedTrue}
	}
	out.WriteByte('\n')
	if _, werr := cmd.OutOrStdout().Write(out.Bytes()); werr != nil {
		extra := secretWording(secret, doc)
		if extra != "" {
			extra = "; " + extra
		}
		return &mutationError{
			err:     fmt.Errorf("%s %s succeeded but writing its response failed: %w%s", m.Method, m.Path, werr, extra),
			applied: appliedTrue,
		}
	}
	return nil
}

func jsonSyntaxErr(doc []byte) error {
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return err
	}
	return errors.New("not exactly one JSON value")
}

func secretWording(kind secretKind, doc []byte) string {
	switch kind {
	case secretSPCredential:
		return "the clientSecret in this response cannot be retrieved again; revoke the credential and create a new one"
	case secretAutomationWebhook:
		var v map[string]any
		if json.Unmarshal(doc, &v) == nil {
			for _, k := range []string{"webhookHmacSecret", "webhookCapabilityUrl"} {
				if s, ok := v[k].(string); ok && s != "" {
					return "the response's webhookHmacSecret/webhookCapabilityUrl were not delivered; re-read the automation before relying on them"
				}
			}
		}
	}
	return ""
}

// classifyApplied maps a failed send to `applied` (R32), then widens false to
// unknown when an earlier retried attempt reached the server (R36).
func classifyApplied(err error) appliedState {
	state := appliedFalse
	prior := false
	var apiErr *client.APIError
	var attempt *client.AttemptError
	hasAttempt := errors.As(err, &attempt)
	if hasAttempt {
		prior = attempt.PriorSent
	}
	switch {
	case errors.As(err, &apiErr):
		prior = prior || apiErr.PriorSent
		if s := apiErr.StatusCode; s == http.StatusRequestTimeout || s == 499 || s >= 500 {
			state = appliedUnknown
		}
	case errors.As(err, new(*client.RedirectError)), errors.As(err, new(*client.RedirectLoopError)):
	case hasAttempt && attempt.Sent:
		state = appliedUnknown
	}
	if state == appliedFalse && prior {
		state = appliedUnknown
	}
	return state
}

func notApplied(err error) error {
	if err == nil {
		return nil
	}
	return &mutationError{err: err, applied: appliedFalse}
}
