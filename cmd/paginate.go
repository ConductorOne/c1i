package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/redact"
	"github.com/spf13/cobra"
)

// limitReached reports whether `emitted` rows have hit the requested
// `limit`. A limit of 0 means "no cap"; validateCountFlags rejects a negative
// one before any command runs. Centralizing this check pins the semantics
// across every list command and gives the table tests one place to assert it.
func limitReached(emitted, limit int) bool {
	return limit > 0 && emitted >= limit
}

// effectivePageSize tightens the per-call page size toward `limit` when
// `limit` is smaller than the requested page size, so an ordinary
// `--limit 3` query doesn't fetch 50 items and discard 47 of them. This is
// only correct when every fetched row becomes one written row — that
// one-to-one assumption is what lets "3 remaining" mean "ask for 3 more".
//
// A caller whose rows can be dropped after fetching (a client-side filter,
// or a --fields projection that empties a row — see emitter.Filtered) must
// NOT call this at all while that filtering is active: `written` stays near
// zero while filtered rows keep coming in, so `limit - written` stays near
// `limit` for the whole scan and every page ends up asking for `limit` (or
// fewer) items instead of the real page size — turning what should be a
// handful of full pages into many tiny, mostly-wasted ones. Skip the call
// and pass `requested` straight through instead; see the callers that check
// `clientFilter`/`enc.Filtered()` before calling this.
//
// General rule this function keeps getting fed the wrong counter for: a
// counter driving a REQUEST-SHAPING decision (this function) must count
// rows fetched (or, in the safe 1:1 case, rows written — same number); a
// counter driving a STOP decision (limitReached) must count rows actually
// written. Conflating the two is the recurring bug.
//
// When `limit` is unset (<=0), the requested page-size is returned
// unchanged.
//
// Edge: if the remaining headroom (limit - written) is somehow zero or
// negative (the outer loop should have stopped already), we still return
// at least 1 — better to make a small wasteful call than to send
// pageSize=0 and get an undefined response from the API.
func effectivePageSize(requested, limit, written int) int {
	if limit <= 0 {
		return requested
	}
	remaining := limit - written
	if remaining < 1 {
		return 1
	}
	if remaining < requested {
		return remaining
	}
	return requested
}

// stuckPageRepeats and stuckStaleRows together mark a cursor as stuck: that
// many consecutive stale pages (every item already seen) holding at least
// that many rows in all. One stale page can be a healthy offset-paginated
// server that gained a page of rows between requests; the row floor keeps a
// tiny --page-size on a busy newest-first feed from tripping on a few inserts.
const (
	stuckPageRepeats = 2
	stuckStaleRows   = 25
)

// maxIdlePages backstops a cursor that keeps changing without bringing a new
// row (or, for id-less rows, any cursor that never ends): it counts requests
// since the last page with an unseen id. No healthy list sends 100k pages
// without one; the audit log, whose rows carry no id, opts out
// (newUncappedPaginationCursorGuard).
const maxIdlePages = 100_000

// maxEmptyPages bounds a run of consecutive empty pages under fresh tokens.
// A healthy server sends at most a few before its last page; 100 leaves room
// for sparse server-side filtering.
const maxEmptyPages = 100

// paginationCursorGuard detects auto-pagination that stopped making progress:
// a page_token sent twice, a run of stale or empty pages under fresh-looking
// tokens, or maxPages requests without a new id. Construct with
// newPaginationCursorGuard.
type paginationCursorGuard struct {
	seenTokens   map[string]bool
	seenIDs      map[string]bool
	stale        int
	staleRows    int
	empty        int
	idle         int
	pages        int
	maxPages     int    // 0 = no idle or empty-page cap
	compositeIDs bool   // see withCompositeIDs
	stopReason   string // the cap that ended the run; "" for a repeat
}

func newPaginationCursorGuard() *paginationCursorGuard {
	return &paginationCursorGuard{seenTokens: map[string]bool{}, seenIDs: map[string]bool{}, maxPages: maxIdlePages}
}

// newUncappedPaginationCursorGuard is for a stream with no natural size bound
// (the audit log): only the repeated-token and stale-page checks apply, since
// a quiet stretch of the log may page through many empty results.
func newUncappedPaginationCursorGuard() *paginationCursorGuard {
	g := newPaginationCursorGuard()
	g.maxPages = 0
	return g
}

// tokenAlreadyUsed reports whether token was already sent, or the idle-page
// cap is spent. Call it once before each request, so a repeat is caught
// without re-fetching the page.
func (g *paginationCursorGuard) tokenAlreadyUsed(token string) bool {
	g.idle++
	if g.maxPages > 0 && g.idle > g.maxPages {
		g.stopReason = fmt.Sprintf("no new rows in %d pages", g.maxPages)
		return true
	}
	return token != "" && g.seenTokens[token]
}

// markTokenUsed records the token the request just sent.
func (g *paginationCursorGuard) markTokenUsed(token string) {
	if token != "" {
		g.seenTokens[token] = true
	}
}

// withCompositeIDs marks ids built from several fields (or expanded objects),
// where a row missing one is plausible: a page of only id-less rows is then
// emitted rather than rejected as an unrecognized shape.
func (g *paginationCursorGuard) withCompositeIDs() *paginationCursorGuard {
	g.compositeIDs = true
	return g
}

// checkPage takes a page's row ids ("" for a row without one) and returns
// which rows to emit. A page with any unseen id is emitted whole, since ids
// need not be unique. A page whose ids were all seen is stale: only its
// id-less rows, which can't be shown to be repeats, are emitted. The listing
// ends once a stale run meets stuckPageRepeats and stuckStaleRows, or after
// maxEmptyPages empty pages in a row. Unless withCompositeIDs, a non-empty
// page with no id at all means the command no longer finds the id it reads,
// so it fails rather than print unchecked rows. A nil cmd skips the stderr
// warning.
func (g *paginationCursorGuard) checkPage(cmd *cobra.Command, route string, ids []string) ([]bool, error) {
	g.pages++
	keep := make([]bool, len(ids))
	fresh, known := false, 0
	for i, id := range ids {
		if id == "" {
			keep[i] = true
			continue
		}
		known++
		if !g.seenIDs[id] {
			fresh = true
			g.seenIDs[id] = true
		}
	}
	switch {
	case len(ids) == 0:
		return keep, g.countEmpty(cmd, route)
	case known == 0 && g.compositeIDs:
		g.empty = 0
		return keep, nil
	case known == 0:
		return nil, g.shapeError(cmd, route, len(ids))
	}
	g.empty = 0
	if !fresh {
		g.stale++
		g.staleRows += known
		if g.stale >= stuckPageRepeats && g.staleRows >= stuckStaleRows {
			return nil, g.warnStuck(cmd, route)
		}
		return keep, nil
	}
	g.stale, g.staleRows, g.idle = 0, 0, 0
	for i := range keep {
		keep[i] = true
	}
	return keep, nil
}

// inStaleRun reports whether no page since the last new id brought one.
func (g *paginationCursorGuard) inStaleRun() bool {
	return g.stale > 0
}

// shapeError is the exit-6 error for a page with rows but no ids; past the
// first page it also reports the result as incomplete.
func (g *paginationCursorGuard) shapeError(cmd *cobra.Command, route string, rows int) error {
	problem := fmt.Sprintf("%d rows came back without the id c1i reads; the response shape is not one this command recognizes", rows)
	if g.pages == 1 {
		return &nonJSONResponseError{fmt.Errorf("%s: %s", route, problem)}
	}
	if cmd != nil {
		notice(cmd, noticeInfo, fmt.Sprintf(
			"Warning: results from %s are INCOMPLETE: %s. Rows already printed are kept.", route, problem), nil)
	}
	return &nonJSONResponseError{fmt.Errorf("results from %s are INCOMPLETE: %s", route, problem)}
}

// checkIDlessPage is checkPage for a command whose rows carry no id: only the
// empty-page bound applies.
func (g *paginationCursorGuard) checkIDlessPage(cmd *cobra.Command, route string, rows int) error {
	if rows > 0 {
		g.empty = 0
		return nil
	}
	return g.countEmpty(cmd, route)
}

func (g *paginationCursorGuard) countEmpty(cmd *cobra.Command, route string) error {
	g.empty++
	if g.maxPages == 0 || g.empty < maxEmptyPages {
		return nil
	}
	g.stopReason = fmt.Sprintf("%d empty pages in a row", g.empty)
	return g.warnStuck(cmd, route)
}

// keepRows returns the rows checkPage marked to emit.
func keepRows[T any](rows []T, keep []bool) []T {
	out := make([]T, 0, len(rows))
	for i, r := range rows {
		if keep[i] {
			out = append(out, r)
		}
	}
	return out
}

// stuckError is the exit-6 error for this guard's stop, naming the cap when
// that is what tripped.
func (g *paginationCursorGuard) stuckError(route string) error {
	return &stuckPaginationCursorError{route: route, reason: g.stopReason}
}

// warnStuck prints the stuck-cursor warning (unless cmd is nil) and returns
// stuckError.
func (g *paginationCursorGuard) warnStuck(cmd *cobra.Command, route string) error {
	if cmd != nil {
		reason := "the server's pagination cursor stopped advancing (a repeated nextPageToken, or repeated pages of rows already seen)"
		if g.stopReason != "" {
			reason = g.stopReason
		}
		notice(cmd, noticeInfo, fmt.Sprintf(
			"Warning: results from %s are INCOMPLETE: %s. Rows already printed are kept.", route, reason), nil)
	}
	return g.stuckError(route)
}

// pageSpec describes one auto-paginated listing whose page items decode to T.
// runPages is the only pagination loop; every list command, and the paged
// reads in api and auth login, express their request and row shape here.
type pageSpec[T any] struct {
	// Route names the request in warnings, e.g. "GET /api/v1/apps".
	Route string
	// Fetch requests one page of up to size items from token ("" = first).
	// It wraps its own errors; runPages returns them as given.
	Fetch pageFetch
	// Parse decodes a page into its items and next-page token, wrapping its
	// own errors.
	Parse func(data []byte) (items []T, next string, err error)
	// ID names an item for the stuck-cursor guard ("" = none). Nil means the
	// rows carry no id, so only the repeated-token and empty-page bounds apply.
	ID func(T) string
	// Rows turns one item into the rows to emit: none to skip it, several to
	// expand it. Nil emits the item itself.
	Rows func(T) []any
	// Sink receives each row; nil encodes to Enc.
	Sink func(any) error

	Enc       *emitter // nil: no --limit and no projection-aware sizing
	Limit     int
	PageSize  int
	Token     string // first page's token (--page-token)
	Manual    bool   // --page-token was given: fetch exactly one page
	FixedSize bool   // send PageSize as is, never tightened toward Limit
	Composite bool   // ids are built from several fields; see withCompositeIDs
	Uncapped  bool   // the stream has no natural bound (audit log)
	// HoldStale defers a page of rows already seen until a later page brings
	// a new one or the list ends, instead of dropping it: a raw passthrough
	// prints an isolated repeat, while a stuck run errors out first.
	HoldStale bool
}

// pageFetch requests one page of up to size items.
type pageFetch func(c *client.Client, size int, token string) ([]byte, error)

// emitItems writes the rows of items to sink until limit rows have been
// written to enc, and reports whether it has. A nil enc means no limit; nil
// rows emits each item as is.
func emitItems[T any](enc *emitter, limit int, sink func(any) error, rows func(T) []any, items []T) (bool, error) {
	for _, item := range items {
		out := []any{item}
		if rows != nil {
			out = rows(item)
		}
		for _, row := range out {
			if err := sink(row); err != nil {
				return false, err
			}
			if enc != nil && limitReached(enc.Written(), limit) {
				return true, nil
			}
		}
	}
	return false, nil
}

// emitRows prints items through enc, stopping at limit rows.
func emitRows[T any](enc *emitter, limit int, items []T, rows func(T) []any) error {
	_, err := emitItems(enc, limit, enc.Encode, rows, items)
	return err
}

// collectPages runs s to completion and returns every kept item: no output,
// no --limit, and no stderr warning, for a read another command builds on.
// Rows is ignored: the items themselves are returned.
func collectPages[T any](c *client.Client, s pageSpec[T]) ([]T, error) {
	var all []T
	s.Rows = nil
	s.Sink = func(row any) error {
		all = append(all, row.(T))
		return nil
	}
	if err := runPages(nil, c, s); err != nil {
		return nil, err
	}
	return all, nil
}

// runPages runs spec to completion: fetch, parse, guard, emit, and follow the
// cursor until it ends, --limit is met, or the guard calls the cursor stuck.
func runPages[T any](cmd *cobra.Command, c *client.Client, s pageSpec[T]) error {
	var guard *paginationCursorGuard
	if s.Uncapped {
		guard = newUncappedPaginationCursorGuard()
	} else {
		guard = newPaginationCursorGuard()
	}
	if s.Composite {
		guard.withCompositeIDs()
	}
	written := func() int {
		if s.Enc == nil {
			return 0
		}
		return s.Enc.Written()
	}
	sink := s.Sink
	if sink == nil {
		sink = s.Enc.Encode
	}
	emit := func(items []T) (bool, error) { return emitItems(s.Enc, s.Limit, sink, s.Rows, items) }
	var held []T
	// fail prints rows held back before an error that isn't the guard's: they
	// were deferred only until the run proved it wasn't stuck.
	fail := func(err error) error {
		if _, wErr := emit(held); wErr != nil {
			return errors.Join(err, wErr)
		}
		return err
	}

	token := s.Token
	for s.Enc == nil || !limitReached(written(), s.Limit) {
		if guard.tokenAlreadyUsed(token) {
			return guard.warnStuck(cmd, s.Route)
		}
		size := s.PageSize
		if !s.FixedSize && s.Enc != nil && !s.Enc.Filtered() {
			size = effectivePageSize(s.PageSize, s.Limit, written())
		}
		data, err := s.Fetch(c, size, token)
		if err != nil {
			return fail(err)
		}
		items, next, err := s.Parse(data)
		if err != nil {
			return fail(err)
		}
		if s.ID == nil {
			if err := guard.checkIDlessPage(cmd, s.Route, len(items)); err != nil {
				return err
			}
		} else {
			ids := make([]string, len(items))
			for i, item := range items {
				ids[i] = s.ID(item)
			}
			keep, err := guard.checkPage(cmd, s.Route, ids)
			if err != nil {
				return err
			}
			if !s.HoldStale {
				items = keepRows(items, keep)
			}
		}
		if s.HoldStale {
			if guard.inStaleRun() && next != "" {
				held = append(held, items...)
				items = nil
			} else {
				items, held = append(held, items...), nil
			}
		}
		if done, err := emit(items); err != nil || done {
			return err
		}
		if next == "" || s.Manual {
			return nil
		}
		guard.markTokenUsed(token)
		token = next
	}
	return nil
}

// listClientFor builds cmd's list client; call it before reading the
// command's own flags so an auth failure keeps its place in the error order.
func listClientFor(cmd *cobra.Command) (*client.Client, error) {
	return listClientVia(cmd, newListClient)
}

// listClientVia is listClientFor with the command family's own constructor
// (newPoliciesClient), so a test seam on it keeps working.
func listClientVia(cmd *cobra.Command, build func(*cobra.Command, string) (*client.Client, error)) (*client.Client, error) {
	baseURL, err := GetBaseURL()
	if err != nil {
		return nil, err
	}
	c, err := build(cmd, baseURL)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	return c, nil
}

// listPages runs s with the standard list flags (--page-size, --page-token,
// --limit) and the NDJSON emitter.
func listPages[T any](cmd *cobra.Command, c *client.Client, s pageSpec[T]) error {
	s.Enc = newEmitter(cmd)
	s.Limit = getIntFlag(cmd, "limit")
	s.PageSize = pageSizeFlag(cmd)
	s.Token, _ = cmd.Flags().GetString("page-token")
	s.Manual = cmd.Flags().Changed("page-token")
	return runPages(cmd, c, s)
}

// getPage fetches a page with GET, paging by the snake_case query params
// page_size/page_token alongside extra.
func getPage(ctx context.Context, path string, extra map[string]string) pageFetch {
	return func(c *client.Client, size int, token string) ([]byte, error) {
		data, err := c.Get(ctx, path, pageParams(size, token, extra))
		if err != nil {
			return nil, fmt.Errorf("API error: %w", err)
		}
		return data, nil
	}
}

// pageParams are the snake_case query params that page a GET list.
func pageParams(size int, token string, extra map[string]string) map[string]string {
	params := map[string]string{"page_size": strconv.Itoa(size)}
	if token != "" {
		params["page_token"] = token
	}
	for k, v := range extra {
		params[k] = v
	}
	return params
}

// postPage fetches a page with POST, paging by pageSize/pageToken in a copy
// of base.
func postPage(ctx context.Context, path string, base map[string]any) pageFetch {
	return postPageBody(ctx, path, func(size int, token string) map[string]any {
		return pagedBody(base, size, token)
	})
}

// postPageBody is postPage for a request whose body the command builds.
func postPageBody(ctx context.Context, path string, body func(size int, token string) map[string]any) pageFetch {
	return func(c *client.Client, size int, token string) ([]byte, error) {
		data, err := c.Post(ctx, path, body(size, token))
		if err != nil {
			return nil, fmt.Errorf("API error: %w", err)
		}
		return data, nil
	}
}

// pagedBody copies base and sets its paging keys.
func pagedBody(base map[string]any, size int, token string) map[string]any {
	body := cloneJSONObject(base)
	body["pageSize"] = size
	if token != "" {
		body["pageToken"] = token
	}
	return body
}

// parseKeyed decodes {"<key>": [...], "nextPageToken": "..."} into typed
// items. With several keys the first non-empty array wins.
func parseKeyed[T any](keys ...string) func([]byte) ([]T, string, error) {
	return func(data []byte) ([]T, string, error) {
		var page map[string]json.RawMessage
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, "", fmt.Errorf("failed to parse response: %w", err)
		}
		var items []T
		for _, key := range keys {
			v, ok := page[key]
			if !ok {
				continue
			}
			var got []T
			if err := json.Unmarshal(v, &got); err != nil {
				return nil, "", fmt.Errorf("failed to parse response: %w", err)
			}
			if len(items) == 0 {
				items = got
			}
		}
		var next string
		if v, ok := page["nextPageToken"]; ok {
			if err := json.Unmarshal(v, &next); err != nil {
				return nil, "", fmt.Errorf("failed to parse response: %w", err)
			}
		}
		return items, next, nil
	}
}

// parseList is parseKeyed for the usual "list" key.
func parseList[T any](data []byte) ([]T, string, error) { return parseKeyed[T]("list")(data) }

type listRowFn func(map[string]any) map[string]any

// mapPages is the spec for a list whose rows are built from generic objects
// under listKey. idKeys name the built row's fields that together identify
// it for the stuck-cursor guard; none when rows have no id.
func mapPages(route, listKey string, idKeys []string, row listRowFn, fetch pageFetch) pageSpec[map[string]any] {
	s := pageSpec[map[string]any]{
		Route:     route,
		Fetch:     fetch,
		Composite: len(idKeys) > 1,
		Parse: func(data []byte) ([]map[string]any, string, error) {
			items, next, err := extractListAndToken(data, listKey)
			if err != nil {
				return nil, "", fmt.Errorf("failed to parse response: %w", err)
			}
			rows := make([]map[string]any, len(items))
			for i, raw := range items {
				var item map[string]any
				if err := json.Unmarshal(raw, &item); err != nil {
					return nil, "", fmt.Errorf("failed to parse response item: %w", err)
				}
				rows[i] = row(item)
			}
			return rows, next, nil
		},
	}
	if len(idKeys) > 0 {
		s.ID = func(r map[string]any) string { return rowGuardID(r, idKeys) }
	}
	return s
}

// listMapPages pages a list built by mapPages.
func listMapPages(cmd *cobra.Command, route, listKey string, idKeys []string, row listRowFn, fetch pageFetch) error {
	c, err := listClientFor(cmd)
	if err != nil {
		return err
	}
	return listPages(cmd, c, mapPages(route, listKey, idKeys, row, fetch))
}

// postBodyList pages a POST list/search whose filter comes from --body-file;
// the paging keys are the flags' (see ownBodyKeys).
func postBodyList(cmd *cobra.Command, path string, request map[string]any, row listRowFn, idKeys ...string) error {
	return listMapPages(cmd, "POST "+path, "list", idKeys, row, postPage(cmd.Context(), path, request))
}

// rowGuardID joins a row's idKeys values with scopedRowID; "" if any is
// missing or there are none.
func rowGuardID(row map[string]any, idKeys []string) string {
	if len(idKeys) == 0 {
		return ""
	}
	parts := make([]string, len(idKeys))
	for i, k := range idKeys {
		parts[i], _ = row[k].(string)
	}
	return scopedRowID(parts...)
}

func getFixedList(cmd *cobra.Command, path string, row listRowFn) error {
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Get(cmd.Context(), path, nil)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	items, next, err := extractListAndToken(data, "list")
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	enc := newEmitter(cmd)
	limit := getIntFlag(cmd, "limit")
	for _, raw := range items {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("failed to parse response item: %w", err)
		}
		if err := enc.Encode(row(item)); err != nil {
			return err
		}
		if limitReached(enc.Written(), limit) {
			return nil
		}
	}
	// These routes don't paginate today, so a cursor isn't followed; it still
	// must not pass as a complete result.
	if next != "" {
		notice(cmd, noticeInfo, fmt.Sprintf(
			"Warning: GET %s returned a partial result — the server reported more pages, which c1i does not fetch for this command.", redact.URL(path)), nil)
	}
	return nil
}

// rowFn adapts a one-row-per-item builder to pageSpec.Rows.
func rowFn[T, R any](build func(T) R) func(T) []any {
	return func(item T) []any { return []any{build(item)} }
}
