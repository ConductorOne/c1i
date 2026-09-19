package client

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// seqRT returns a programmed status code per call and records each request's
// body and Authorization header, so a test can assert both the retry decision
// and that a rewound body (and a fresh bearer) was resent intact.
type seqRT struct {
	codes  []int
	calls  int
	bodies []string
	auths  []string
}

func (s *seqRT) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		body = string(b)
	}
	s.bodies = append(s.bodies, body)
	s.auths = append(s.auths, req.Header.Get("Authorization"))
	code := s.codes[s.calls]
	s.calls++
	return &http.Response{StatusCode: code, Body: http.NoBody, Header: make(http.Header)}, nil
}

func newReq(t *testing.T, body string) *http.Request {
	t.Helper()
	var r *http.Request
	var err error
	if body == "" {
		r, err = http.NewRequest(http.MethodGet, "https://x/y", nil)
	} else {
		r, err = http.NewRequest(http.MethodPost, "https://x/y", strings.NewReader(body))
	}
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRetryOnTokenReject_401ThenSuccess(t *testing.T) {
	base := &seqRT{codes: []int{401, 200}}
	inv := 0
	rt := &retryOnTokenReject{base: base, invalidate: func() { inv++ }}
	resp, err := rt.RoundTrip(newReq(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200 after re-mint", resp.StatusCode)
	}
	if inv != 1 {
		t.Errorf("invalidate calls = %d, want 1", inv)
	}
	if base.calls != 2 {
		t.Errorf("base calls = %d, want 2", base.calls)
	}
}

func TestRetryOnTokenReject_SecondRejectIsReturned(t *testing.T) {
	base := &seqRT{codes: []int{401, 401}}
	inv := 0
	rt := &retryOnTokenReject{base: base, invalidate: func() { inv++ }}
	resp, err := rt.RoundTrip(newReq(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want the second 401 returned", resp.StatusCode)
	}
	if inv != 1 || base.calls != 2 {
		t.Errorf("invalidate=%d base=%d, want exactly one retry (1 and 2)", inv, base.calls)
	}
}

func TestRetryOnTokenReject_NoRetryOnOtherStatuses(t *testing.T) {
	for _, code := range []int{200, 403, 404, 500} {
		base := &seqRT{codes: []int{code}}
		inv := 0
		rt := &retryOnTokenReject{base: base, invalidate: func() { inv++ }}
		resp, err := rt.RoundTrip(newReq(t, ""))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != code || inv != 0 || base.calls != 1 {
			t.Errorf("code %d: got status=%d inv=%d calls=%d, want pass-through (no invalidate, one call)", code, resp.StatusCode, inv, base.calls)
		}
	}
}

func TestRetryOnTokenReject_ReplaysBody(t *testing.T) {
	base := &seqRT{codes: []int{401, 200}}
	rt := &retryOnTokenReject{base: base, invalidate: func() {}}
	if _, err := rt.RoundTrip(newReq(t, `{"k":"v"}`)); err != nil {
		t.Fatal(err)
	}
	if len(base.bodies) != 2 {
		t.Fatalf("captured %d bodies, want 2", len(base.bodies))
	}
	if base.bodies[0] != `{"k":"v"}` || base.bodies[1] != `{"k":"v"}` {
		t.Errorf("bodies = %q, want the same JSON resent on retry", base.bodies)
	}
}

// rotatingTokenSource models cacheTokenSource after an invalidation. The retry
// must invoke it again, not let an outer oauth2.ReuseTokenSource resend stale.
type rotatingTokenSource struct {
	invalidated bool
}

func (s *rotatingTokenSource) Token() (*oauth2.Token, error) {
	token := "stale"
	if s.invalidated {
		token = "fresh"
	}
	return &oauth2.Token{AccessToken: token, TokenType: "Bearer"}, nil
}

// TestRetryOnTokenReject_ThroughOAuth2Transport drives the real oauth2.Transport
// beneath retryOnTokenReject (the exact composition client.New builds), proving
// a replayed POST gets a freshly attached bearer after cache invalidation.
func TestRetryOnTokenReject_ThroughOAuth2Transport(t *testing.T) {
	base := &seqRT{codes: []int{401, 200}}
	source := &rotatingTokenSource{}
	oauthT := &oauth2.Transport{Source: source, Base: base}
	rt := &retryOnTokenReject{
		base: oauthT,
		invalidate: func() {
			source.invalidated = true
		},
	}
	req, err := http.NewRequest(http.MethodPost, "https://x/y", strings.NewReader(`{"k":"v"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	if len(base.bodies) != 2 || base.bodies[0] != `{"k":"v"}` || base.bodies[1] != `{"k":"v"}` {
		t.Errorf("bodies=%q, want the JSON resent on retry", base.bodies)
	}
	if len(base.auths) != 2 || base.auths[0] != "Bearer stale" || base.auths[1] != "Bearer fresh" {
		t.Errorf("auth headers=%q, want stale then fresh bearer", base.auths)
	}
}

// unrewindableReq has a body but no GetBody, so it cannot be replayed.
func TestRetryOnTokenReject_NoRetryWhenBodyUnrewindable(t *testing.T) {
	base := &seqRT{codes: []int{401, 200}}
	inv := 0
	rt := &retryOnTokenReject{base: base, invalidate: func() { inv++ }}
	req, err := http.NewRequest(http.MethodPost, "https://x/y", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Body = io.NopCloser(strings.NewReader("payload"))
	req.GetBody = nil
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 || inv != 0 || base.calls != 1 {
		t.Errorf("got status=%d inv=%d calls=%d, want the first 401 returned with no retry", resp.StatusCode, inv, base.calls)
	}
}
