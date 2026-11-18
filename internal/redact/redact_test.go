package redact

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestURLMasksSecretsAndKeepsTheRest(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		leaks []string // must not appear
		keeps []string // must still appear
	}{
		{"url secret query name spellings", "https://h.example/x?api_key=Aleak&Access-Token=Bleak&ok=2", []string{"Aleak", "Bleak"}, []string{"ok=2"}},
		{"semicolon query and fragment", "https://h.example/x?a=1;token=Q1leak&b=2#access_token=Q2leak&c=3", []string{"Q1leak", "Q2leak"}, []string{"a=1", "b=2", "c=3"}},
		{"malformed url keeps no userinfo", "https://us er:PWleak@h.example/x?api_key=Q3leak", []string{"PWleak", "Q3leak"}, nil},
		{"scheme-less userinfo", "user:PWleak@h.example/x?secret=Q4leak&z=9", []string{"PWleak", "Q4leak"}, []string{"z=9"}},
		{"username-only userinfo (PAT)", "https://ghp_SU1leak@github.example/x", []string{"ghp_SU1leak"}, []string{"github.example/x"}},
		{"username-only userinfo, malformed", "https://ghp_SU2leak@gith ub.example/x", []string{"ghp_SU2leak"}, nil},
		{"scheme-relative userinfo", "//u:PW3leak@h.example/x", []string{"PW3leak"}, []string{"h.example/x"}},
		{"key/sig/signature params", "https://h.example/x?key=P1leak&sig=P2leak&Signature=P3leak&X-Amz-Signature=P4leak&ok=1", []string{"P1leak", "P2leak", "P3leak", "P4leak"}, []string{"ok=1"}},
		{"semicolon before query", "https://h.example/p;token=M1leak?a=1", []string{"M1leak"}, nil},
		{"? inside a secret value", "https://h.example/x?token=abc?defleak", []string{"abc?defleak", "defleak"}, nil},
		{"? inside a secret value, neighbours kept", "https://h.example/x?a=1&token=x?yleak&b=2", []string{"yleak"}, []string{"a=1", "b=2"}},
		{"? inside a secret value, fragment", "https://h.example/x#a=1&token=x?zleak&b=2", []string{"zleak"}, []string{"a=1", "b=2"}},
		{"non-secret a=b=c unchanged", "https://h.example/x?a=b=c&d=1", nil, []string{"a=b=c", "d=1"}},
		{"path-only semicolon param", "https://h.example/p;api_key=M2leak/x", []string{"M2leak"}, nil},
		{"scheme-less token@host", "TOKENleak@h.example/p", []string{"TOKENleak"}, []string{"h.example/p"}},
		{"; inside a query value", "https://h.example/x?token=a;bleak&ok=1", []string{"bleak"}, []string{"ok=1"}},
		{"# inside a query value", "https://h.example/x?token=a#cleak", []string{"cleak"}, nil},
		{"percent-encoded pair", "https://h.example/x?token%3Dpleak&ok=1", []string{"pleak"}, []string{"ok=1"}},
		{"; separated pair after a plain one", "https://h.example/x?a=1;token=sleak", []string{"sleak"}, []string{"a=1"}},
		{"userinfo and query", "https://user:PWleak@h.example/x?token=QTleak&keep=1", []string{"PWleak", "QTleak", "user"}, []string{"h.example/x", "keep=1"}},
		{"PAT with x-oauth-basic", "https://PAT1leak:x-oauth-basic@h.example/x", []string{"PAT1leak"}, []string{"h.example/x"}},
		{"PAT with empty password", "https://PAT2leak:@h.example/x", []string{"PAT2leak"}, []string{"h.example/x"}},
		{"PAT with password, malformed", "https://PAT3leak:p w@gith ub.example/x", []string{"PAT3leak"}, nil},
		{"PAT with password, scheme-less", "PAT4leak:pw@h.example/x", []string{"PAT4leak"}, []string{"h.example/x"}},
		{"git user with a password keeps the user", "https://git:PW5leak@h.example/x", []string{"PW5leak"}, []string{"git:"}},
		{"; path param before a slash", "https://h.example/x;token=S1leak/y?ok=1", []string{"S1leak"}, []string{"/y?ok=1"}},
		{"%3B path param", "https://h.example/x%3Btoken=S2leak?ok=1", []string{"S2leak"}, []string{"ok=1"}},
		{"%3b path param, lower case", "https://h.example/x%3btoken=S2bleak/y", []string{"S2bleak"}, []string{"/y"}},
		{"encoded key in a ; path param", "https://h.example/x;%74oken=S3leak/y", []string{"S3leak"}, []string{"/y"}},
		{"encoded key, malformed value escape", "https://h.example/x?%74oken=S4%zzleak&ok=1", []string{"S4%zzleak", "S4"}, []string{"ok=1"}},
		{"encoded ; inside a path param key", "https://h.example/x;a%3Btoken=S6leak/y", []string{"S6leak"}, []string{"/y"}},
		{"git@ is not a secret", "ssh://git@github.example/org/repo", nil, []string{"ssh://git@github.example/org/repo"}},
		// An id holding "?k=v" is sent with its "?" encoded.
		{"%3F query inside a path segment", "https://h.example/api/v1/apps/x%3Faccess_token=E1leak", []string{"E1leak"}, []string{"/apps/x%3Faccess_token="}},
		{"%3f lower case, neighbours kept", "https://h.example/a/x%3fa=1&token=E2leak&b=2/y?ok=1", []string{"E2leak"}, []string{"x%3fa=1&", "&b=2/y?ok=1"}},
		{"%26 separated pair after %3F", "https://h.example/a/x%3Fa=1%26token=E3leak%26b=2", []string{"E3leak"}, []string{"a=1%26", "%26b=2"}},
		{"%3D separator after %3F", "https://h.example/a/x%3Ftoken%3DE4leak/y", []string{"E4leak"}, []string{"x%3Ftoken%3D", "/y"}},
		{"%23 fragment inside a path segment", "https://h.example/a/x%23access_token=E5leak", []string{"E5leak"}, nil},
		{"encoded key after %3F", "https://h.example/a/x%3F%74oken=E6leak", []string{"E6leak"}, nil},
		{"non-secret %3F pair unchanged", "https://h.example/a/x%3Fa=1%26b=2/y", nil, []string{"https://h.example/a/x%3Fa=1%26b=2/y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := URL(tc.url)
			for _, l := range tc.leaks {
				if strings.Contains(got, l) {
					t.Errorf("URL(%q) = %q leaked %q", tc.url, got, l)
				}
			}
			for _, k := range tc.keeps {
				if !strings.Contains(got, k) {
					t.Errorf("URL(%q) = %q lost %q", tc.url, got, k)
				}
			}
		})
	}
}

// One sentinel: a URL password prints as [redacted], not url.Redacted's xxxxx.
func TestURLPasswordUsesTheSingleSentinel(t *testing.T) {
	got := URL("https://user:PWleak@h.example/x")
	if strings.Contains(got, "xxxxx") || strings.Contains(got, "PWleak") || !strings.Contains(got, "[redacted]") {
		t.Errorf("URL = %q", got)
	}
}

func TestPathMasksParamsAndKeepsTheRest(t *testing.T) {
	got := Path("/api/v1/x;token=SEKL4/y;ok=1")
	if strings.Contains(got, "SEKL4") || !strings.Contains(got, ";ok=1") {
		t.Errorf("Path = %q", got)
	}
	if got := Path("/api/v1/apps/x%3Faccess_token=SEKL5"); got != "/api/v1/apps/x%3Faccess_token="+Placeholder {
		t.Errorf("Path = %q", got)
	}
}

// A server error body that echoes a path whose id holds an encoded query.
func TestTextMasksQueryEncodedInAPathSegment(t *testing.T) {
	in := `{"message":"not found: /api/v1/apps/x%3Faccess_token=SEKT1"} and /a/b%3Ftoken%3DSEKT2.`
	got := Text(in)
	if strings.Contains(got, "SEKT") || !strings.Contains(got, "/apps/x%3Faccess_token=") || !strings.HasSuffix(got, ".") {
		t.Errorf("Text(%q) = %q", in, got)
	}
	if in := "see /a/x%3Fa=1 for details"; Text(in) != in {
		t.Errorf("Text(%q) = %q", in, Text(in))
	}
}

func TestTextMasksURLShapedSubstrings(t *testing.T) {
	cases := []struct {
		name, in string
		leaks    []string
		keeps    []string
	}{
		{"path query", `bad request /api/v1/x?token=SEK1&ok=2 failed`, []string{"SEK1"}, []string{"/api/v1/x?token=[redacted]&ok=2", "bad request", "failed"}},
		{"full url semicolon", `see https://h.example/path;token=SEK2 now`, []string{"SEK2"}, []string{"https://h.example/path;token=[redacted]", "now"}},
		{"full url userinfo and query", `got https://u:SEK3@h.example/x?api_key=SEK4&a=1.`, []string{"SEK3", "SEK4"}, []string{"a=1."}},
		{"scheme-relative", `//h.example/x?secret=SEK5&b=1`, []string{"SEK5"}, []string{"b=1"}},
		{"bare query", `retry with ?access_token=SEK6&z=1 later`, []string{"SEK6"}, []string{"z=1", "later"}},
		{"bare pairs", `token=SEK7&x=1`, []string{"SEK7"}, []string{"x=1"}},
		{"bare pairs, secret last", `x=1&password=SEK8`, []string{"SEK8"}, []string{"x=1"}},
		{"encoded pair", `/x?token%3DSEK9&ok=1`, []string{"SEK9"}, []string{"ok=1"}},
		{"quoted url", `"https://h.example/x?token=SEK10"`, []string{"SEK10"}, []string{`"https://h.example/x?token=[redacted]`}},
		{"two urls", `a https://h/x?token=SEK11 b /y?key=SEK12 c`, []string{"SEK11", "SEK12"}, []string{" b ", " c"}},
		{"path param then slash", `/api/x;token=SEK13/y?ok=1`, []string{"SEK13"}, []string{"/y?ok=1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.in)
			for _, l := range tc.leaks {
				if strings.Contains(got, l) {
					t.Errorf("Text(%q) = %q leaks %q", tc.in, got, l)
				}
			}
			for _, k := range tc.keeps {
				if !strings.Contains(got, k) {
					t.Errorf("Text(%q) = %q lost %q", tc.in, got, k)
				}
			}
		})
	}
}

func TestTextLeavesOrdinaryTextAlone(t *testing.T) {
	for _, in := range []string{
		"", "plain prose, no urls.", "status code=404 for tokenType=Bearer", "and/or maybe? yes",
		"https://h.example/x?a=1&b=2", "/api/v1/apps/123", "token=lonely in prose", "a=1&b=2",
		"user@example.com wrote", "read /etc/hosts; then exit", "1/2 or 3/4?",
	} {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestBody(t *testing.T) {
	t.Run("json string values masked, shape kept", func(t *testing.T) {
		in := `{"message":"bad /api/v1/x?token=SEK1&ok=2","code":3,"n":1.50,"nested":{"u":"https://h/p;token=SEK2"},"list":["/y?key=SEK3&k=1"]}`
		got := Body(in)
		if !json.Valid([]byte(got)) {
			t.Fatalf("invalid JSON: %s", got)
		}
		for _, l := range []string{"SEK1", "SEK2", "SEK3"} {
			if strings.Contains(got, l) {
				t.Errorf("leaks %s: %s", l, got)
			}
		}
		for _, k := range []string{`ok=2`, `"code":3`, `1.50`, `k=1`, `[redacted]`} {
			if !strings.Contains(got, k) {
				t.Errorf("lost %q: %s", k, got)
			}
		}
	})
	t.Run("clean json is byte-identical", func(t *testing.T) {
		in := `{ "b": 1,  "a": "/x?ok=1" }`
		if got := Body(in); got != in {
			t.Errorf("Body = %q", got)
		}
	})
	t.Run("non-json falls back to Text", func(t *testing.T) {
		got := Body("<html>/x?token=SEK4</html>")
		if strings.Contains(got, "SEK4") {
			t.Errorf("leaks: %s", got)
		}
	})
	t.Run("json scalar number is text", func(t *testing.T) {
		if got := Body("42"); got != "42" {
			t.Errorf("Body = %q", got)
		}
	})
	t.Run("key echoing a url", func(t *testing.T) {
		if got := Body(`{"/x?token=SEK5":1}`); strings.Contains(got, "SEK5") {
			t.Errorf("leaks: %s", got)
		}
	})
}

func TestBodyNeverReturnsAnUnmaskedSecret(t *testing.T) {
	for _, in := range []string{
		`{"a":1}} /x?token=SEK1`,
		`[1]] https://h/x?token=SEK2`,
		`""} /p?a=1&token=SEK3&b=2`,
		`{"m":"/x?token=SEK4&a=1","m":"ok"}`,
		`{"token":"SEK5","token":""}`,
		`{"error":"x","client_secret":"SEK6"}`,
		`{"access_token":"SEK7"}`,
		`{"o":{"password":"SEK8"},"l":[{"api_key":"SEK9"}]}`,
		`{"token":{"inner":"SEK10"}}`,
		`{"m":"a=1%26token%3DSEK11"}`,
	} {
		if got := Body(in); strings.Contains(got, "SEK") {
			t.Errorf("Body(%q) = %q leaks", in, got)
		} else if strings.HasPrefix(in, "{") && !strings.Contains(in, "}}") && !json.Valid([]byte(got)) {
			t.Errorf("Body(%q) = %q is not valid JSON", in, got)
		}
	}
	if got := Body(`{"code":5,"message":"x","n":1.50}`); got != `{"code":5,"message":"x","n":1.50}` {
		t.Errorf("a numeric code was changed: %s", got)
	}
	if got := Body(`{"a":1,"a":2,"/x?token=SEKK":3}`); strings.Contains(got, "SEKK") {
		t.Errorf("key leaks: %s", got)
	}
}

func TestTextHandlesJSONEscapesAndEncodedNesting(t *testing.T) {
	for _, tc := range []struct{ in, keep string }{
		{`/x?a=1\u0026token=SEK1&b=2`, "a=1"},
		{`/x?token\u003dSEK2`, "/x?"},
		{`https:\/\/h\/x?token=SEK3`, `h\/x?token=[redacted]`},
		{`/x?a=1\u0026token\u003dSEK4&b=2`, "b=2"},
		{`/x?a=1%26token%3DSEK5`, "/x?a="},
		{`/login?next=%2Fx%3Ftoken%3DSEK6&z=9`, "z=9"},
		{`/login?next=https%3A%2F%2Fh%2Fx%3Ftoken%3DSEK7`, "/login?next="},
		{`token%3DSEK8&a=1`, "a=1"},
		{`/x?a=%2526token%253DSEK9`, "/x?a="},
		{`https://h/x?next=%2Fy%3Fa%3D1%26k%3D2&ok=1`, "ok=1"},
	} {
		got := Text(tc.in)
		if strings.Contains(got, "SEK") || !strings.Contains(got, tc.keep) {
			t.Errorf("Text(%q) = %q, want no SEK and %q", tc.in, got, tc.keep)
		}
	}
	// Encoded text with no secret in it is left alone.
	if in := `https://h/x?next=%2Fy%3Fa%3D1%26k%3D2&ok=1`; Text(in) != in {
		t.Errorf("Text(%q) = %q", in, Text(in))
	}
}

func TestIsSecretKeyCoversCredentialNames(t *testing.T) {
	for _, k := range []string{"client_assertion", "clientAssertion", "device_code", "deviceCode", "x-amz-signature", "X-Amz-Credential", "x-amz-security-token", "X-Amz-Signature", "sig", "signature"} {
		if !IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = false", k)
		}
	}
	for _, k := range []string{"client_assertion_type", "device_code_ttl", "tokenType", "author"} {
		if IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = true", k)
		}
	}
}

func TestTextKeepsEmailAfterQuestionMark(t *testing.T) {
	for _, in := range []string{"?user@example.com", "who is it? user@example.com", "x ?a@b.example ok"} {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) = %q", in, got)
		}
	}
}

// A guard that cannot flake: a generous absolute bound plus a linearity ratio
// (4x the input must cost well under 12x the time), both coarse enough for
// -race and parallel sweeps yet far below the old per-call replacer cost.
func TestBodyStaysLinearOnADenseBody(t *testing.T) {
	dense := func(size int) (jsonBody, text string) {
		var q strings.Builder
		q.WriteString("/x?")
		for i := 0; q.Len() < size; i++ {
			fmt.Fprintf(&q, "k%d=v&", i)
		}
		q.WriteString("token=SEKP")
		var b strings.Builder
		b.WriteString(`{"items":[`)
		for b.Len() < size/2 {
			b.WriteString(`{"some_field":"see /x?a=1&token=SEKQ&k=2 and https://h.example/p;api_key=SEKR","n":1},`)
		}
		b.WriteString(`{"q":"` + q.String() + `"}]}`)
		return b.String(), q.String()
	}
	timeIt := func(in string) time.Duration {
		start := time.Now()
		if got := Body(in); strings.Contains(got, "SEK") {
			t.Error("leaks")
		}
		return time.Since(start)
	}
	j1, t1 := dense(1 << 20)
	j4, t4 := dense(4 << 20)
	for name, p := range map[string][2]string{"json": {j1, j4}, "text": {t1, t4}} {
		d1, d4 := timeIt(p[0]), timeIt(p[1])
		if !raceEnabled && d4 > 10*time.Second {
			t.Errorf("%s: %d bytes took %s", name, len(p[1]), d4)
		}
		if d1 > 5*time.Millisecond && d4 > 12*d1 {
			t.Errorf("%s: 4x input took %s vs %s (not linear)", name, d4, d1)
		}
	}
}

func TestBodyMasksEscapesStringifiedAndScalars(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"nested json, newline before name", `{"message":"{\"detail\":\"bad\\nclient_assertion=SEKR1&a=1\"}"}`},
		{"string json, tab and encoded pair", `{"message":"\"\\tsig%3DSEKR2,\"","code":3}`},
		{"truncated, newline before code", `{"message":"line1\ncode=SEKR3&state=xyz","more`},
		{"triple stringified", `"\"\\\"sig=SEKR4\\\\u0026a=1\\\"\""`},
		{"device_code after backspace", `{"m":"x\bdevice_code=SEKR5&a=1"}`},
		{"pwd after form feed, unicode escape", `{"m":"x\fpwd=SEKR6\u0026a=1"}`},
		{"numeric token", `{"token":12345,"p":"/x?token=SEKR7"}`},
		{"numeric otp", `{"otp":123456}`},
		{"bool password", `{"o":{"password":true}}`},
		{"truncated numeric token", `{"token": 123456789`},
		{"scalar under secret object", `{"credentials":{"n":424242}}`},
	} {
		got := Body(tc.in)
		if strings.Contains(got, "SEKR") || strings.Contains(got, "12345") || strings.Contains(got, "123456") || strings.Contains(got, "424242") || strings.Contains(got, "true") {
			t.Errorf("%s: Body(%q) = %q leaks", tc.name, tc.in, got)
		}
	}
	for _, tc := range []struct{ in, keep string }{
		{`{"message":"{\"detail\":\"bad\\nclient_assertion=SEKR&a=1\"}"}`, "a=1"},
		{`{"message":"line1\ncode=SEKR&state=xyz","more`, "state=xyz"},
	} {
		if got := Body(tc.in); !strings.Contains(got, tc.keep) {
			t.Errorf("Body(%q) = %q lost %q", tc.in, got, tc.keep)
		}
	}
}

func TestCodeFieldStaysVisibleButCodeParamDoesNot(t *testing.T) {
	for _, in := range []string{`{"code":5,"message":"m"}`, `{"code":"not_found"}`, `{"error":{"code":"E_1","n":1}}`, `{"message":"m","code": 7`, `{"code":"not_found"`} {
		if got := Body(in); got != in {
			t.Errorf("Body(%q) = %q, want unchanged", in, got)
		}
	}
	if got := Body(`{"code":"x","token":"SEKR"}`); strings.Contains(got, "SEKR") || !strings.Contains(got, `"code":"x"`) {
		t.Errorf("got %s", got)
	}
	if got := Text("/x?code=SEKR&a=1"); strings.Contains(got, "SEKR") || !strings.Contains(got, "a=1") {
		t.Errorf("code param: %s", got)
	}
	if !IsSecretKey("code") {
		t.Error("code must stay a secret name")
	}
}

// Each case needs jsonPairRE: the text is not whole JSON, so only the pair rule
// can see the secret field.
func TestTextMasksSecretJSONFieldsInBrokenJSON(t *testing.T) {
	long := strings.Repeat("a", 70) + "_token"
	for _, in := range []string{
		`{"client_secret":"SEKR1"`,
		`{"access_token":"SEKR2","error":"x"`,
		`{"token":"SE\"KR3"`,
		`{"token":"SE\"KR4", "a":"b"`,
		`{"` + long + `": "SEKR5"`,
		`\\\"token\\\":\\\"SEKR6`,
		`{\"token\":\"SEKR7\",\"a\":1`,
		`{"token": 123456789 , "n": 1`,
		`{"nested": {"password": "SEKR9"`,
	} {
		if got := Text(in); strings.Contains(got, "SEKR") || strings.Contains(got, "123456789") {
			t.Errorf("Text(%q) = %q leaks", in, got)
		}
	}
	if in := `{"tokenType":"Bearer","n":1`; Text(in) != in {
		t.Errorf("Text(%q) = %q", in, Text(in))
	}
}

func TestBracketedNamesAreSecret(t *testing.T) {
	for _, k := range []string{"a[token]", "token[]", "a[b][password]", "api_key[0]", "x[client_secret]"} {
		if !IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = false", k)
		}
	}
	for _, k := range []string{"a[b]", "tokenType[x]", "a[]", "[]"} {
		if IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = true", k)
		}
	}
	for _, in := range []string{"/x?a[token]=SEKR&ok=1", "/x?a%5Btoken%5D=SEKR&ok=1", "/x?token[]=SEKR&ok=1", "/x?token%5B%5D=SEKR&ok=1"} {
		for name, got := range map[string]string{"URL": URL("https://h.example" + in), "Text": Text(in)} {
			if strings.Contains(got, "SEKR") || !strings.Contains(got, "ok=1") {
				t.Errorf("%s(%q) = %q", name, in, got)
			}
		}
	}
	if got := Path("/p;a[token]=SEKR/y"); strings.Contains(got, "SEKR") {
		t.Errorf("Path = %q", got)
	}
}

func FuzzText(f *testing.F) {
	for _, s := range []string{"", "/x?", " ", "&", `&`, "%26", "https://u:", `\/`, ";"} {
		f.Add(s, s)
	}
	shapes := append(escapedNameShapes(), []string{"/x?a=1&token=SEK", "https://h.example/p;token=SEK/y", "?access_token=SEK&z=1", "u?a=1%26token%3DSEK", `/x?a=1&token=SEK`, "a=1&password=SEK", "https://SEK@h.example/x", "https://u:SEK@h.example/x", "/x?next=%2Fy%3Ftoken%3DSEK", "/cb?redirect=/y?token=SEK&z=1", "bad ?/p?auth=SEK", "/cb?r=https://o.example/y?access_token=SEK"}...)
	f.Fuzz(func(t *testing.T, pre, post string) {
		if strings.Contains(pre+post, "SEK") {
			t.Skip()
		}
		for _, sh := range shapes {
			// A leading "?"/"/"/"=" run in pre can join the shape to a word; the
			// separator keeps the shape its own token.
			in := pre + " " + sh + " " + post
			if got := Text(in); strings.Contains(got, "SEK") {
				t.Fatalf("Text(%q) = %q", in, got)
			}
			if got := Body(in); strings.Contains(got, "SEK") {
				t.Fatalf("Body(%q) = %q", in, got)
			}
			j, _ := json.Marshal(map[string]string{pre: pre + " " + sh + " " + post})
			if got := Body(string(j)); strings.Contains(got, "SEK") {
				t.Fatalf("Body(%s) = %q", j, got)
			}
			if got := Body(string(j) + post); strings.Contains(got, "SEK") {
				t.Fatalf("Body(%s%s) = %q", j, post, got)
			}
		}
	})
}

// escapedNameShapes puts a JSON escape letter right before each exact-match
// secret name, the way a server's "\n" or "\t" lands in a body.
func escapedNameShapes() []string {
	var out []string
	for _, esc := range []string{`\n`, `\t`, `\r`, `\b`, `\f`, `\u000a`, `\\n`} {
		for _, name := range []string{"code", "sig", "client_assertion", "device_code", "pwd", "otp", "auth", "dsn", "token", "a[token]"} {
			out = append(out, "x"+esc+name+"=SEK&a=1", "x"+esc+name+"%3DSEK,")
		}
	}
	return out
}

func FuzzBody(f *testing.F) {
	for _, s := range []string{"", `"`, `\`, `{`, `}`, `,`, `\"`, `\u0000`} {
		f.Add(s, s)
	}
	shapes := append(escapedNameShapes(), `/x?a=1&token=SEK`, `https://h.example/p;token=SEK`, `a=1&password=SEK`)
	f.Fuzz(func(t *testing.T, pre, post string) {
		if strings.Contains(pre+post, "SEK") {
			t.Skip()
		}
		enc := func(v any) string { b, _ := json.Marshal(v); return string(b) }
		for _, sh := range shapes {
			msg := pre + " " + sh + " " + post
			once := enc(map[string]string{"m": msg})
			twice := enc(map[string]string{"m": once})
			for _, in := range []string{
				once, twice, enc(twice), enc([]string{msg}), once[:len(once)-2], twice[:len(twice)-3],
				enc(map[string]any{"token": "SEK", "p": pre}), enc(map[string]any{"a": map[string]any{"password": 424242, "n": post}}),
				`{"token": SEK`, once + post,
			} {
				if got := Body(in); strings.Contains(got, "SEK") {
					t.Fatalf("Body(%q) = %q", in, got)
				}
			}
		}
	})
}

// Decoding is for detection only: text with nothing to mask comes back as sent.
func TestCleanInputsWithBackslashesAreByteIdentical(t *testing.T) {
	for _, in := range []string{
		`path C:\new\temp failed`,
		`regex ^\d+ and C:\\Users\\x`,
		`unicode \u00e9 and quote \"x\"`,
		`tab\there and /x?a=1&b=C:\new`,
		`{"message":"file C:\\new\\temp not found"}`,
		`{"message":"line1\nline2 \"q\" \u00e9","n":1}`,
		`{"message":"{\"detail\":\"C:\\\\new\"}"}`,
		`{"a":"/x?p=1\u0026q=2"}`,
		`[1,"\u00e9",{"k":"\t"}]`,
		`{"truncated":"C:\new\temp`,
	} {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) = %q", in, got)
		}
		if got := Body(in); got != in {
			t.Errorf("Body(%q) = %q", in, got)
		}
	}
}

func TestTextIsIdempotentOnMaskedOutput(t *testing.T) {
	if in := "see ?token=[redacted]"; Text(in) != in {
		t.Errorf("Text(%q) = %q", in, Text(in))
	}
	for _, in := range []string{"see ?token=abc]", "[see /x?token=abc] now", "/x?a=1&token=abc))", `{"token":"SEKI"}`, "x[a=1&password=SEKI]"} {
		once := Text(in)
		if twice := Text(once); twice != once || strings.Contains(once, "SEKI") {
			t.Errorf("Text(%q) = %q, Text again = %q", in, once, twice)
		}
	}
}

func TestBracketedKeyIsNotASecretButBracketedTokenIs(t *testing.T) {
	for _, in := range []string{"/x?sort[key]=2&ok=1", "/x?sort%5Bkey%5D=2&ok=1"} {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) = %q", in, got)
		}
		if u := "https://h.example" + in; URL(u) != u {
			t.Errorf("URL(%q) = %q", u, URL(u))
		}
	}
	if IsSecretKey("sort[key]") || IsSecretKey("key") {
		t.Error("key is a URL-parameter-only name and never secret through IsSecretKey")
	}
	for in, want := range map[string]string{
		"/x?a%5Btoken%5D=SEKR&ok=1": "/x?a%5Btoken%5D=[redacted]&ok=1",
		"/x?a[token]=SEKR&ok=1":     "/x?a[token]=[redacted]&ok=1",
		"/x?token%3DSEKR&ok=1":      "/x?token%3D[redacted]&ok=1",
	} {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecretFieldsHoldingObjectsOrArraysInBrokenJSON(t *testing.T) {
	for _, in := range []string{
		`{"auth":{"type":"basic","value":"SEKA"}`,
		`{"token":["SEKB"]}{}`,
		`{"error":"x"}{"access_token":["SEKC"]}`,
		`ok "token": ["SEKE"]`,
		`{"credentials":{"a":{"b":["SEKF",{"c":"}"}]}},"n":"x"}`,
		`{"token":{"a":"SEKH` + "\n",
	} {
		if got := Text(in); strings.Contains(got, "SEK") {
			t.Errorf("Text(%q) = %q leaks", in, got)
		}
	}
	if got := Text(`{"error":"x","token":{"a":1},"keep":"y"}{`); !strings.Contains(got, `"keep":"y"`) || strings.Contains(got, `"a":1`) {
		t.Errorf("got %q", got)
	}
}

// Whatever the input, output that differs from it carries a mask.
func FuzzUnmaskedInputIsReturnedAsSent(f *testing.F) {
	for _, s := range []string{`C:\new\temp`, `{"m":"a\nb"}`, `/x?a=1`, `https://git@h/x`, `\u00e9 \"q\"`, `{"token":1}`, `a=1&b=2`, `?user@example.com`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		for name, got := range map[string]string{"Text": Text(in), "Body": Body(in)} {
			if got != in && !strings.Contains(got, Placeholder) {
				t.Fatalf("%s(%q) = %q: changed without a mask", name, in, got)
			}
		}
	})
}

func TestNestedUnencodedURLInQueryValueIsMasked(t *testing.T) {
	for _, in := range []string{
		`https://h.example/cb?redirect=/y?token=SEK3&z=1`,
		`/cb?redirect=https://o.example/y?access_token=SEK4`,
		`bad ?/p?auth=SEK2`,
		`{"detail":"?/p?Authorization=QZXSEK}{{\"z\":1}"}`,
		`/a?b=/c?d=/e?f=/g?token=SEK5&k=1`,
		`/a?b=c#frag?token=SEK6`,
	} {
		for name, got := range map[string]string{"Text": Text(in), "URL": URL("https://h.example" + in)} {
			if strings.Contains(got, "SEK") {
				t.Errorf("%s(%q) = %q leaks", name, in, got)
			}
		}
	}
	if got := Text(`https://h.example/cb?redirect=/y?token=SEK3&z=1`); !strings.Contains(got, "z=1") || !strings.Contains(got, "redirect=/y?token=") {
		t.Errorf("got %q", got)
	}
}

// Everything but the masked values is as sent: no decoding of unrelated text.
func TestMaskedTextKeepsUnrelatedBytes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`C:\new\temp failed for /x?token=SEK8`, `C:\new\temp failed for /x?token=[redacted]`},
		{`{"m":"C:\\new"} {"token":"SEK7"}`, `{"m":"C:\\new"} {"token":"[redacted]"}`},
		{`{"m":"{\"auth\":{\"k\":\"SEK2\"}}"}`, `{"m":"{\"auth\":[redacted]}"}`},
		{`bell \u001b[0m then /x?password=SEK9&a=1`, `bell \u001b[0m then /x?password=[redacted]&a=1`},
		{`a\/b /x?a=1\u0026token=SEK10\u0026b=2 \u00e9`, `a\/b /x?a=1\u0026token=[redacted]\u0026b=2 \u00e9`},
		{`{"m":"x\"y","token":"SE\"K11"}`, `{"m":"x\"y","token":"[redacted]"}`},
	} {
		if got := Text(tc.in); got != tc.want {
			t.Errorf("Text(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

func TestPlaceholderPrefixedValueIsStillMasked(t *testing.T) {
	for _, in := range []string{`{"token":"[redacted]SEK13"`, `{"token":"[redacted]SEK14"}`} {
		if got := Text(in); strings.Contains(got, "SEK") {
			t.Errorf("Text(%q) = %q leaks", in, got)
		}
	}
	if in := `{"token":"[redacted]","n":1`; Text(in) != in {
		t.Errorf("Text(%q) = %q", in, Text(in))
	}
}

func TestBareKeyBaseNameIsSecretButInnerSegmentIsNot(t *testing.T) {
	if got := Text("/x?key[0]=SEKK&ok=1"); strings.Contains(got, "SEKK") || !strings.Contains(got, "ok=1") {
		t.Errorf("key[0]: %q", got)
	}
	if got := URL("https://h.example/x?key[]=SEKK&ok=1"); strings.Contains(got, "SEKK") {
		t.Errorf("key[]: %q", got)
	}
	if in := "/x?sort[key]=2&ok=1"; Text(in) != in {
		t.Errorf("sort[key]: %q", Text(in))
	}
}

// Property: Text only ever replaces spans with [redacted]. Rebuilding the input
// from the output (pairing each placeholder with a removed span) must give the
// input back, and no injected secret may survive.
func TestSpliceChangesOnlyMaskedSpans(t *testing.T) {
	secrets := []string{"SEKA1", "SEKB2", "SEKC3"}
	frames := []string{
		"pre C:\\new\\t /x?token=%s post", `{"a":"x","password":"%s"}`, `/x?a=1&token=%s&b=2`, `{"m":"x","token":"%s","n":"C:\\u00e9"}`,
		`see https://h.example/p;token=%s/y ok`, `{"m":"/x?a=1\u0026token=%s\u0026b=2 \t"}`, `?/p?auth=%s tail\n`,
		`{"credentials":{"k":"%s"},"z":"C:\\w"} x`,
	}
	for _, fr := range frames {
		for _, sec := range secrets {
			in := strings.ReplaceAll(fr, "%s", sec)
			got := Text(in)
			if strings.Contains(got, sec) {
				t.Errorf("Text(%q) = %q leaks", in, got)
				continue
			}
			if strings.Count(got, Placeholder) < 1 {
				t.Errorf("Text(%q) = %q: no mask", in, got)
				continue
			}
			// Un-mask: put the secret back where the placeholders are; what is
			// left of the frame must be the input.
			if back := strings.ReplaceAll(got, Placeholder, sec); back != in && !strings.Contains(fr, "credentials") {
				t.Errorf("Text(%q) = %q changed more than the secret", in, got)
			}
		}
	}
}

func FuzzSplice(f *testing.F) {
	for _, s := range []string{"", `\n`, `\u00e9`, `"`, `C:\new`, "x y", "&", "/"} {
		f.Add(s, s)
	}
	frames := []string{`%s /x?token=SEKS&a=1 %s`, `{"p":"%s","token":"SEKS","q":"%s"}`, `%s ?/p?auth=SEKS %s`, `%s {"credentials":{"k":"SEKS"}} %s`, `%s /x?a=1\u0026password=SEKS\u0026b=2 %s`}
	f.Fuzz(func(t *testing.T, pre, post string) {
		if strings.Contains(pre+post, "SEK") || strings.Contains(pre+post, Placeholder) {
			t.Skip()
		}
		for _, fr := range frames {
			in := strings.Replace(strings.Replace(fr, "%s", pre, 1), "%s", post, 1)
			got := Text(in)
			if strings.Contains(got, "SEKS") {
				t.Fatalf("Text(%q) = %q leaks", in, got)
			}
			if !strings.Contains(got, Placeholder) {
				t.Fatalf("Text(%q) = %q: not masked", in, got)
			}
		}
	})
}

// A detected value is replaced only where it sits in a value position; the same
// text elsewhere is as sent.
func TestSpliceTouchesOnlyValuePositions(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"a":"v1","token":"1","count":11} prose 1 and 1, id=21 end 1`, `{"a":"v1","token":"[redacted]","count":11} prose 1 and 1, id=21 end 1`},
		{`{"token":"abc","m":"x"} ... see abc and abcd`, `{"token":"[redacted]","m":"x"} ... see abc and abcd`},
		{`see abc then /x?token=abc&n=abcd`, `see abc then /x?token=[redacted]&n=abcd`},
		{`1 2 3 /x?token=2 and 2 and a2b`, `1 2 3 /x?token=[redacted] and 2 and a2b`},
		{`{\"token\":\"xyz\"} xyz`, `{\"token\":\"[redacted]\"} xyz`},
		{`x=1 /x?a=1&token=1&n=1`, `x=[redacted] /x?a=[redacted]&token=[redacted]&n=[redacted]`}, // other values equal to it
		{`/x?a=1&token=1 and 1`, `/x?a=[redacted]&token=[redacted] and 1`},
	} {
		got := Text(tc.in)
		if got != tc.want {
			t.Errorf("Text(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
		if again := Text(got); again != got {
			t.Errorf("not stable: Text(%q) = %q", got, again)
		}
	}
}

func TestSecondCopyAndEscapedBoundaries(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// the fallback path (decoded text) still masks a second value-position copy
		{`https:\/\/SQABC123@h.example\/p other="SQABC123" end`, `https:\/\/[redacted]@h.example\/p other="[redacted]" end`},
		{`{"detail":"?/p?Authorization=QZXMARK}{{\"z\":1}"}`, `{"detail":"?/p?Authorization=[redacted]\"z\":1}"}`},
		// slash-escaped "//" and wide-escaped terminators splice in place, byte-exact
		{`https:\/\/SQABC123@h.example\/p`, `https:\/\/[redacted]@h.example\/p`},
		{`/x?a=1&token=SQABC123&n=2 tail`, `/x?a=1&token=[redacted]&n=2 tail`},
		{`{"m":"/x?a=1\\u0026token=SQABC123\\u0026n=2"}`, `{"m":"/x?a=1\\u0026token=[redacted]\\u0026n=2"}`},
		{`{"m":"https:\\/\\/h\\/x;token=SQABC123\\\/y"}`, `{"m":"https:\\/\\/h\\/x;token=[redacted]\\\/y"}`},
	} {
		got := Text(tc.in)
		if strings.Contains(got, "SQABC") || strings.Contains(got, "QZXMARK") {
			t.Errorf("Text(%q) = %q leaks", tc.in, got)
		}
		if got != tc.want {
			t.Errorf("Text(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

// A second value-position copy of a secret, under minimal escaping at levels
// 0-3, comes back byte-exact; under wide escaping no secret survives.
func FuzzSecondCopyEscaping(f *testing.F) {
	for i := uint8(0); i < 4; i++ {
		f.Add("ABC1", i, false, uint8(0))
		f.Add("x", i, true, uint8(1))
		f.Add("", i, true, uint8(2))
	}
	frames := []string{
		`https://SEC@h.example/p other="SEC" end`,
		`/x?a=1&token=SEC&n=2 and key:"SEC" end`,
		`{"m":"x","token":"SEC","other":"SEC"}`,
		`see https://h.example/p;token=SEC/y and v=SEC.`,
	}
	f.Fuzz(func(t *testing.T, suffix string, level uint8, wide bool, frame uint8) {
		for _, r := range suffix {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
				t.Skip()
			}
		}
		sec := "SQABC" + suffix
		fr := frames[int(frame)%len(frames)]
		in, want := strings.ReplaceAll(fr, "SEC", sec), strings.ReplaceAll(fr, "SEC", Placeholder)
		for i := 0; i < int(level%4); i++ {
			in, want = escapeJSON(in, wide), escapeJSON(want, wide)
		}
		got := Text(in)
		if strings.Contains(got, "SQABC") {
			t.Fatalf("Text(%q) = %q leaks", in, got)
		}
		if !wide && got != want {
			t.Fatalf("Text(%q)\n got %q\nwant %q", in, got, want)
		}
	})
}

// URLValue redacts URL-shaped strings only: a preview must show an
// email-shaped display name exactly as it will be sent.
func TestURLValueMasksOnlyURLShaped(t *testing.T) {
	kept := []string{"zz12l4@example.com", "Ada Lovelace <ada@example.invalid>", "not a url"}
	for _, in := range kept {
		if got := URLValue(in); got != in {
			t.Errorf("URLValue(%q) = %q, want unchanged", in, got)
		}
	}
	redacted := map[string]string{
		"https://user:PWleak@h.example/x?token=Qleak": "PWleak",
		"TOKENleak@h.example/p":                       "TOKENleak",
		"user:PWleak@h.example/x":                     "PWleak",
		"//u:PWleak@h.example/x":                      "PWleak",
		"h.example/cb?token=Qleak":                    "Qleak",
		"h.example/x;api_key=Qleak":                   "Qleak",
		"x?client_secret=Qleak":                       "Qleak",
		"note u=//svc:Qleak@egress.example/x":         "Qleak",
		"see //TOKENleak@host.example/p":              "TOKENleak",
	}
	for in, leak := range redacted {
		if got := URLValue(in); strings.Contains(got, leak) || !strings.Contains(got, Placeholder) {
			t.Errorf("URLValue(%q) = %q", in, got)
		}
	}
}
