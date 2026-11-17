// Package redact masks credentials in URLs. It is the one rule behind --dry-run
// previews and --debug / error output, so they never disagree.
package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Placeholder replaces every masked value.
const Placeholder = "[redacted]"

// FoldKey folds a JSON key's camelCase, snake_case and kebab-case spellings
// (the API accepts the first two) into one lookup form.
func FoldKey(k string) string { return strings.ToLower(foldReplacer.Replace(k)) }

var foldReplacer = strings.NewReplacer("_", "", "-", "")

var (
	// A folded name is secret when it ends with one of these...
	secretSuffixes = []string{"token", "secret", "secretkey", "password", "passphrase", "signature", "apikey", "privatekey", "accesskey", "signingkey", "encryptionkey", "sshkey", "credential", "credentials", "bearer"}
	// ...or equals one of these (names the suffixes miss, incl. the MCP server
	// builders' headerValue/credentialsJson/secretAccessKey; cmd pins that).
	secretExact = map[string]bool{
		"auth": true, "authorization": true, "code": true, "sig": true,
		"pwd": true, "pass": true, "otp": true, "jwt": true, "hmac": true, "cookie": true, "setcookie": true,
		"connectionstring": true, "dsn": true, "clientassertion": true, "devicecode": true,
		"headervalue": true, "credentialsjson": true, "secretaccesskey": true,
	}
)

// IsSecretKey is the one rule for a credential-shaped name, shared by body
// fields and URL parameters. It is best-effort by name; tokenType, secretName
// and author stay visible because only suffixes and exact names count. A
// bracketed name ("a[token]", "token[]", "a%5Btoken%5D" once decoded) is secret
// when its base or any "[...]" segment is.
func IsSecretKey(k string) bool { return anySegment(k, secretName) }

func secretName(k string) bool {
	f := FoldKey(k)
	if secretExact[f] {
		return true
	}
	for _, suf := range secretSuffixes {
		if strings.HasSuffix(f, suf) {
			return true
		}
	}
	return false
}

// anySegment reports fn true for k or for the base or a "[...]" segment of it.
func anySegment(k string, fn func(string) bool) bool {
	if fn(k) {
		return true
	}
	if !strings.ContainsAny(k, "[]") {
		return false
	}
	for _, seg := range strings.FieldsFunc(k, func(r rune) bool { return r == '[' || r == ']' }) {
		if fn(seg) {
			return true
		}
	}
	return false
}

// isSecretParam adds the names that are secret only as a URL parameter: a
// body field called "key" is an ordinary field.
func isSecretParam(k string) bool {
	base, _, _ := strings.Cut(k, "[") // "key[0]" is the parameter key; "sort[key]" is not
	return IsSecretKey(k) || FoldKey(base) == "key"
}

// isSecretField is the rule for a name in a response body: "code" stays
// visible there (gRPC "code":5, third-party "not_found"), though it is secret
// as a URL parameter.
func isSecretField(k string) bool {
	return anySegment(k, func(n string) bool { return secretName(n) && FoldKey(n) != "code" })
}

// URL masks a URL's userinfo (the password and the username, which may be a
// PAT as in `https://<token>@host` or `https://<token>:x-oauth-basic@host`;
// the conventional non-secret "git" user is kept) and the value of every
// secret-named query, fragment or `;` parameter, encoded values included (a
// secret inside a percent-encoded nested URL) and pairs after an encoded "?"
// or "#" in a path segment (see maskEncodedQueries), leaving the rest as sent. A
// string that does not parse as a URL with a host goes through urlText instead,
// so a malformed or scheme-less one fails closed.
func URL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return urlText(s)
	}
	if u.User != nil {
		name := maskedUser(u.User.Username())
		if _, has := u.User.Password(); has {
			u.User = url.UserPassword(name, Placeholder)
		} else {
			u.User = url.User(name)
		}
		s = strings.ReplaceAll(u.String(), "%5Bredacted%5D", Placeholder)
	}
	return maskEncodedValues(maskSecretPairs(s), maxEscapeLevels)
}

const gitUser = "git"

// maskedUser keeps only the conventional non-secret "git" user.
func maskedUser(name string) string {
	if name == gitUser {
		return name
	}
	return Placeholder
}

var (
	urlPasswordRE  = regexp.MustCompile(`//([^/?#:@]*):[^/?#]*@`)
	urlUsernameRE  = regexp.MustCompile(`(//)([^/?#:@]+)@`)
	bareUserinfoRE = regexp.MustCompile(`^([^/@\s:]+):[^/@\s]*@`)
	bareTokenRE    = regexp.MustCompile(`^([^/@\s:]+)@([^/@\s]*\.[^/@\s]*)`)
	pathParamRE    = regexp.MustCompile(`(?i)(;|%3B)([^=;/?#\s]+)(?:=|%3D)([^;/?#\s]*)`)
)

// indexEncodedEqual returns the byte index of the first "%3d" or "%3D" in s,
// or -1. It scans bytes: strings.ToUpper can change a string's length (ı -> I),
// which would shift the index off the separator.
func indexEncodedEqual(s string) int {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '%' && s[i+1] == '3' && (s[i+2] == 'd' || s[i+2] == 'D') {
			return i
		}
	}
	return -1
}

// urlText is URL's fail-closed fallback for text url.Parse cannot
// use: it masks userinfo (a bare "TOKEN@host.tld/..." too, which also hits an
// email-shaped string in URL position) and the secret-named parameters.
func urlText(s string) string {
	s = urlPasswordRE.ReplaceAllStringFunc(s, func(m string) string {
		return "//" + maskedUser(m[2:strings.Index(m, ":")]) + ":" + Placeholder + "@"
	})
	s = urlUsernameRE.ReplaceAllStringFunc(s, func(m string) string {
		return "//" + maskedUser(m[2:len(m)-1]) + "@"
	})
	s = bareUserinfoRE.ReplaceAllStringFunc(s, func(m string) string {
		return maskedUser(m[:strings.Index(m, ":")]) + ":" + Placeholder + "@"
	})
	s = bareTokenRE.ReplaceAllStringFunc(s, func(m string) string {
		user, host, _ := strings.Cut(m, "@")
		return maskedUser(user) + "@" + host
	})
	return maskEncodedValues(maskSecretPairs(s), maxEscapeLevels)
}

// maskSecretPairs masks secret-named parameters. In a query only "&" ends a
// pair, so a ";", "#" or "?" inside a secret value cannot leave part of it
// showing; a fragment's pairs end at "&" or ";"; path ";k=v" parameters end at
// "/". Pairs are also tried percent-decoded ("token%3Dx"). The rest is as sent.
func maskSecretPairs(s string) string { return maskPairsDepth(s, maxNestedQueries) }

// maxNestedQueries bounds how many "?" inside values restart a query.
const maxNestedQueries = 4

func maskPairsDepth(s string, depth int) string {
	end := strings.IndexAny(s, "?#")
	head, tail := s, ""
	if end >= 0 {
		head, tail = s[:end], s[end:]
	}
	head = pathParamRE.ReplaceAllStringFunc(head, func(m string) string {
		sepLen := 1
		if m[0] == '%' {
			sepLen = 3
		}
		rest := m[sepLen:]
		iEq := strings.IndexByte(rest, '=')
		iEnc := indexEncodedEqual(rest)
		nameEnd, sepEnd := -1, 0
		switch {
		case iEq >= 0 && (iEnc < 0 || iEq < iEnc):
			nameEnd, sepEnd = iEq, iEq+1
		case iEnc >= 0:
			nameEnd, sepEnd = iEnc, iEnc+3
		}
		if nameEnd < 0 || !secretParamName(rest[:nameEnd]) {
			return m
		}
		// Keep the separator as sent; only the value is replaced.
		return m[:sepLen+sepEnd] + Placeholder
	})
	head = maskEncodedQueries(head)
	if tail == "" {
		return head
	}
	inQuery := tail[0] == '?'
	pieces := strings.Split(tail[1:], "&")
	for i, p := range pieces {
		if inQuery {
			if m := maskPair(p); m != p {
				pieces[i] = m
				continue
			}
			if j := strings.IndexAny(p, "?#"); j >= 0 {
				if p[j] == '?' && depth > 0 { // an unencoded nested URL: its own query
					pieces[i] = maskSemi(p[:j]) + "?" + maskPairsDepth("?"+p[j+1:], depth-1)[1:]
				} else {
					pieces[i] = maskSemi(p[:j]) + p[j:j+1] + maskSemi(p[j+1:])
				}
				continue
			}
		}
		pieces[i] = maskSemi(p)
	}
	return head + tail[:1] + strings.Join(pieces, "&")
}

// maskEncodedQueries masks secret pairs after an encoded "?" or "#" inside a
// path segment: an id "x?token=s" is sent as "x%3Ftoken=s". Pairs end at "&",
// ";", "/" or an encoded "&", "?", "#" or ";". One level of encoding only.
func maskEncodedQueries(head string) string {
	if !strings.Contains(head, "%") {
		return head
	}
	segs := strings.Split(head, "/")
	for i, seg := range segs {
		start := -1
		for j := 0; j+2 < len(seg); j++ {
			if seg[j] == '%' && (strings.EqualFold(seg[j+1:j+3], "3F") || seg[j+1:j+3] == "23") {
				start = j
				break
			}
		}
		if start < 0 {
			continue
		}
		var b strings.Builder
		b.WriteString(seg[:start])
		for rest := seg[start:]; rest != ""; {
			if n := pairDelim(rest); n > 0 {
				b.WriteString(rest[:n])
				rest = rest[n:]
				continue
			}
			end := 1
			for end < len(rest) && pairDelim(rest[end:]) == 0 {
				end++
			}
			b.WriteString(maskPair(rest[:end]))
			rest = rest[end:]
		}
		segs[i] = b.String()
	}
	return strings.Join(segs, "/")
}

// pairDelim returns the length of the pair delimiter s starts with, or 0.
func pairDelim(s string) int {
	if s != "" && (s[0] == '&' || s[0] == ';') {
		return 1
	}
	return encodedDelim(s)
}

// encodedDelim returns 3 when s starts with an encoded "&", "?", "#" or ";".
func encodedDelim(s string) int {
	if len(s) < 3 || s[0] != '%' {
		return 0
	}
	for _, d := range []string{"26", "3F", "23", "3B"} {
		if strings.EqualFold(s[1:3], d) {
			return 3
		}
	}
	return 0
}

// maskSemi masks each ";"-separated pair of seg.
func maskSemi(seg string) string {
	parts := strings.Split(seg, ";")
	for i, p := range parts {
		parts[i] = maskPair(p)
	}
	return strings.Join(parts, ";")
}

// maskPair masks the whole of "k=v" when k, percent-decoded, is secret. The
// pair is decoded leniently: a malformed escape in the value must not keep the
// key from being recognized.
func maskPair(p string) string {
	k, _, ok := strings.Cut(lenientUnescape(p), "=")
	if !ok || !secretParamName(k) {
		return p
	}
	// Keep the key and separator as sent ("=" or an encoded "%3D").
	if i := strings.IndexAny(p, "=%"); i >= 0 {
		for ; i < len(p); i++ {
			if p[i] == '=' {
				return p[:i+1] + Placeholder
			}
			if p[i] == '%' && i+2 < len(p) && strings.EqualFold(p[i:i+3], "%3D") {
				return p[:i+3] + Placeholder
			}
		}
	}
	return k + "=" + Placeholder
}

// secretParamName reports a secret parameter name as written or percent-decoded
// (an encoded ";" splits it: "a%3Btoken" ends in "token").
func secretParamName(k string) bool {
	if isSecretParam(k) {
		return true
	}
	d := lenientUnescape(k)
	_, last, _ := strings.Cut(d, ";")
	return isSecretParam(d) || isSecretParam(last)
}

// lenientUnescape is url.QueryUnescape that leaves an invalid escape as is
// instead of failing, so one bad "%zz" cannot hide the rest.
func lenientUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '+':
			b.WriteByte(' ')
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(v))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// Path masks the secret-named ";k=v" parameters of a request path (as
// URL.Path or EscapedPath), the same rule as URL's, encoded values included.
func Path(s string) string { return maskEncodedValues(maskSecretPairs(s), maxEscapeLevels) }

// ErrorURL masks the request URL inside a *url.Error in place, so its text no
// longer carries a secret; the type and wrap chain are unchanged. Call it only
// on an error you own (one fresh from net/http).
func ErrorURL(err error) {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = URL(ue.URL)
	}
}

// Text masks URL- and query-shaped substrings of server-supplied prose (an
// error body that echoes a request): absolute and scheme-relative URLs, a
// "/path?query" or "/path;param" run, a path with an encoded "?" or "#" in
// a segment ("/apps/x%3Ftoken=v"), a "?name=value" query (also one nested,
// unencoded, in a value), a bare "a=1&b=2" run, a percent-encoded
// "name%3Dvalue", and a "name": value pair of a secret-named JSON field
// (text that is not whole JSON, such as a truncated body; "code" excepted, see
// isSecretField). JSON escapes are decoded (repeatedly, up to 4 levels; 5 or
// more is a limit) to DETECT, so "\n", "\u0026" and a stringified body read as the text they stand
// for; a parameter value that percent-decodes (up to 3 levels) into something
// secret is masked whole.
//
// Output: each detected secret value is replaced by [redacted] where it sits
// in a value position of the input (after "=", ":" or "//", before a
// terminator such as "&", a quote, "," or the end; raw, JSON-escaped up to 4
// levels, or percent-encoded); every other byte is as sent, and input with
// nothing to mask is returned as is. The same text outside a value position
// stays, and another value equal to a secret is masked too. If a secret's
// spelling in the input cannot be placed in a value position, the whole text
// is returned decoded, with every secret masked wherever it sits in a value
// position, instead.
//
// Limits: a lone "token=x" in prose is NOT masked (it cannot be told from
// ordinary words), nor is a scheme-less "TOKEN@host", a header-shaped
// "Authorization: Bearer x", an encoded pair in prose not preceded by "=", a
// percent-encoded bracket in a JSON key ({"a%5Btoken%5D":"x"}), the odd names
// "token.=", "token =" and "token%00=", or a secret nested in 4 or more levels
// of redirect URLs. It over-masks the text around "?user@example.com" when
// "=" or "%" follows, a bare "key=1&x=2", and an unbalanced container under a
// secret-named key (masked to the end of the text).
func Text(s string) string { return text(s, maxDecodeDepth) }

const (
	maxDecodeDepth  = 3
	maxEscapeLevels = 4
)

// decodeJSONEscapes decodes every JSON string escape in s once; an
// unrecognized or truncated escape is left as is.
func decodeJSONEscapes(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		switch n := s[i+1]; n {
		case 'n', 't', 'r', 'b', 'f':
			b.WriteByte(map[byte]byte{'n': '\n', 't': '\t', 'r': '\r', 'b': '\b', 'f': '\f'}[n])
		case '"', '\\', '/':
			b.WriteByte(n)
		case 'u':
			r, ok := hex4(s, i+2)
			if !ok {
				b.WriteByte(c)
				continue
			}
			i += 4
			if r >= 0xD800 && r < 0xDC00 && i+6 < len(s) && s[i+2] == '\\' && s[i+3] == 'u' {
				if lo, ok := hex4(s, i+4); ok && lo >= 0xDC00 && lo < 0xE000 {
					r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
					i += 6
				}
			}
			b.WriteRune(r)
		default:
			b.WriteByte(c)
			continue
		}
		i++
	}
	return b.String()
}

func hex4(s string, at int) (rune, bool) {
	if at+4 > len(s) {
		return 0, false
	}
	var v rune
	for i := at; i < at+4; i++ {
		c := s[i]
		switch {
		case '0' <= c && c <= '9':
			v = v<<4 | rune(c-'0')
		case 'a' <= c && c <= 'f':
			v = v<<4 | rune(c-'a'+10)
		case 'A' <= c && c <= 'F':
			v = v<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return v, true
}

// maskJSON masks the value of each secret-named `"name": value` pair and the
// object or array under a secret-named `"name": {` / `"name": [` (from the
// opener to its matching close, or to the end of a truncated text), reporting
// each masked value to add.
func maskJSON(s string, add func(string)) string {
	if !strings.Contains(s, `"`) {
		return s
	}
	return jsonPairRE.ReplaceAllStringFunc(maskJSONContainers(s, add), func(m string) string {
		g := jsonPairRE.FindStringSubmatch(m)
		val := g[2] + g[3]
		if !isSecretField(g[1]) || val == "" || g[3] == "null" || val == Placeholder {
			return m
		}
		add(val)
		return m[:len(m)-len(val)] + Placeholder
	})
}

func maskJSONContainers(s string, add func(string)) string {
	var b strings.Builder
	pos := 0
	for _, m := range jsonContainerRE.FindAllStringSubmatchIndex(s, -1) {
		if m[0] < pos || !isSecretField(s[m[2]:m[3]]) {
			continue
		}
		end := containerEnd(s, m[4])
		if span := s[m[4]:end]; span != Placeholder {
			add(span)
		}
		b.WriteString(s[pos:m[4]])
		b.WriteString(Placeholder)
		pos = end
	}
	if pos == 0 {
		return s
	}
	b.WriteString(s[pos:])
	return b.String()
}

// containerEnd returns the index just past the bracket that closes the one at
// s[i], skipping strings; len(s) if there is none.
func containerEnd(s string, i int) int {
	depth, inStr := 0, false
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case inStr && c == '\\':
			i++
		case inStr:
			inStr = c != '"'
		case c == '"':
			inStr = true
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

// detect scans s (decoded for detection only) and returns the decoded, masked
// text plus every secret value it masked, as exact substrings of the text at
// the level where each was found.
func detect(s string, depth int) (string, []string) {
	var secrets []string
	add := func(v string) {
		if v != "" && v != Placeholder {
			secrets = append(secrets, v)
		}
	}
	n := s
	for i := 0; i < maxEscapeLevels; i++ {
		n = maskJSON(n, add)
		d := decodeJSONEscapes(n)
		if d == n {
			break
		}
		n = d
	}
	n = maskJSON(n, add)
	r := textCandidateRE.ReplaceAllStringFunc(n, func(m string) string {
		core := trimTrailing(m)
		if core == "" {
			return m
		}
		out := maskCandidate(core, depth)
		if out == core || !strings.Contains(out, Placeholder) {
			return m
		}
		addDiff(core, out, add)
		return out + m[len(core):]
	})
	return r, secrets
}

// addDiff reports the substrings of old that became Placeholders in masked;
// if they cannot be told apart, the whole of old.
func addDiff(old, masked string, add func(string)) {
	parts := strings.Split(masked, Placeholder)
	last := parts[len(parts)-1]
	if !strings.HasPrefix(old, parts[0]) {
		add(old)
		return
	}
	rest := old[len(parts[0]):]
	for _, c := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, c)
		if i < 0 {
			add(old)
			return
		}
		add(rest[:i])
		rest = rest[i+len(c):]
	}
	if !strings.HasSuffix(rest, last) {
		add(old)
		return
	}
	add(rest[:len(rest)-len(last)])
}

func text(s string, depth int) string {
	r, secrets := detect(s, depth)
	if len(secrets) == 0 {
		return s
	}
	out := splice(s, secrets)
	if _, left := detect(out, depth); len(left) == 0 {
		return out
	}
	// A secret's spelling was not found in s: the decoded, masked text, with
	// every other value-position copy of each secret masked as well.
	return splice(r, secrets)
}

// splice replaces each form a secret can take with Placeholder, but only where
// it sits in a value position of s: right after "=", ":" (optional space, then
// an optional quote) or "//", and right before a terminator or the end. The
// separators, quotes and terminators may themselves be escaped or
// percent-encoded. The same text elsewhere is left as sent.
func splice(s string, secrets []string) string {
	seen := map[string]bool{}
	var forms []string
	for _, v := range secrets {
		for _, f := range secretForms(v) {
			if f != "" && !seen[f] {
				seen[f] = true
				forms = append(forms, f)
			}
		}
	}
	type span struct{ i, j int }
	var spans []span
	for _, f := range forms {
		for from := 0; from <= len(s)-len(f); {
			k := strings.Index(s[from:], f)
			if k < 0 {
				break
			}
			i := from + k
			if inValuePosition(s, i, i+len(f)) {
				spans = append(spans, span{i, i + len(f)})
			}
			from = i + 1
		}
	}
	sort.Slice(spans, func(a, b int) bool {
		return spans[a].i < spans[b].i || spans[a].i == spans[b].i && spans[a].j > spans[b].j
	})
	var b strings.Builder
	pos := 0
	for _, sp := range spans {
		if sp.i < pos {
			continue
		}
		b.WriteString(s[pos:sp.i])
		b.WriteString(Placeholder)
		pos = sp.j
	}
	b.WriteString(s[pos:])
	return b.String()
}

// alternation is a regexp alternation of every spelling of the bases.
func alternation(bases ...string) string {
	seen := map[string]bool{}
	var forms []string
	for _, b := range bases {
		for _, f := range jsonForms(b) {
			if !seen[f] {
				seen[f] = true
				forms = append(forms, regexp.QuoteMeta(f))
			}
		}
	}
	sort.Slice(forms, func(i, j int) bool {
		return len(forms[i]) > len(forms[j]) || len(forms[i]) == len(forms[j]) && forms[i] < forms[j]
	})
	return strings.Join(forms, "|")
}

var (
	valueBeforeRE = regexp.MustCompile(`(?i)(?:` + alternation("=", "%3D", ":", "%3A", "//") + `)(?:` +
		alternation(" ", "\t", "\n", "\r", "%20") + `)*(?:` + alternation(`"`, "%22") + `)?$`)
	valueAfterRE = regexp.MustCompile(`(?i)^(?:` + alternation("&", ";", "#", ",", "}", "]", " ", "\t", "\n", "\r",
		"/", "@", ".", ")", "'", "!", `"`, "%26", "%3B", "%23", "%22", "%2C", "%2F") + `)`)
)

func inValuePosition(s string, i, j int) bool {
	before := s[max(0, i-96):i]
	after := s[j:min(len(s), j+40)]
	return valueBeforeRE.MatchString(before) && (after == "" || valueAfterRE.MatchString(after))
}

// secretForms is v raw, percent-encoded, and JSON-string-escaped.
func secretForms(v string) []string {
	return append(jsonForms(v), url.QueryEscape(v), url.PathEscape(v))
}

// jsonForms is v and its JSON-string escapes at 1..maxEscapeLevels levels, in
// both a minimal and a wide style: the one source of every spelling splice
// recognizes, for secrets and for the separators around them.
func jsonForms(v string) []string {
	seen := map[string]bool{v: true}
	out := []string{v}
	cur := []string{v}
	for i := 0; i < maxEscapeLevels; i++ {
		var next []string
		for _, f := range cur {
			for _, e := range []string{escapeJSON(f, false), escapeJSON(f, true)} {
				if !seen[e] {
					seen[e] = true
					next = append(next, e)
					out = append(out, e)
				}
			}
		}
		cur = next
	}
	return out
}

// escapeJSON escapes s as the inside of a JSON string; wide also escapes the
// characters some encoders escape ("/", "&", "<", ">", "=", "?", ";", "#", "%").
func escapeJSON(s string, wide bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || wide && strings.ContainsRune("/&<>=?;#%", r):
			if r == '/' && wide {
				b.WriteString(`\/`)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// trimTrailing drops trailing sentence punctuation from a candidate; a "]"
// stays when it closes a "[" inside it (a "[redacted]" value).
func trimTrailing(m string) string {
	for m != "" && strings.IndexByte(trailingPunct, m[len(m)-1]) >= 0 {
		if m[len(m)-1] == ']' && strings.Count(m, "[") >= strings.Count(m, "]") {
			break
		}
		m = m[:len(m)-1]
	}
	return m
}

// maskCandidate masks one matched candidate (no trailing punctuation).
func maskCandidate(core string, depth int) string {
	switch {
	case core[0] == '?' && !strings.ContainsAny(core, "=%"):
		return core // "?user@example.com" is prose, not a query
	case core[0] == '/' || core[0] == '?' || strings.Contains(core, "//"):
		return maskEncodedValues(URL(core), depth)
	case !strings.Contains(core, "="): // name%3Dvalue
		if out := maskPair(core); out != core {
			return out
		}
		if hasSecret(lenientUnescape(core), depth) {
			return Placeholder
		}
		return core
	}
	return maskEncodedValues(maskSecretPairs("?" + core)[1:], depth) // a=1&b=2
}

const trailingPunct = ".,)]}'\"!"

const urlChars = `[^\s"'<>\\]`

var (
	textCandidateRE = regexp.MustCompile(
		`(?i)(?:[a-z][a-z0-9+.-]*:)?//` + urlChars + `+` + // absolute / scheme-relative URL
			`|/[^\s"'<>\\?;]*[?;]` + urlChars + `*` + // /path?query, /path;param
			`|/[^\s"'<>\\?;]*%(?:3[Ff]|23)` + urlChars + `*` + // /path/id%3Fquery
			`|\?` + urlChars + `+` + // ?query
			`|[\w.%\[\]-]+=[^\s&"'<>\\]*(?:&[\w.%\[\]-]+=[^\s&"'<>\\]*)+` + // a=1&b=2
			`|[\w.%\[\]-]+%3[Dd][^\s&"'<>\\]*`) // name%3Dvalue
	// "name": "string value" (escapes and a missing closing quote allowed) or
	// "name": scalar.
	jsonPairRE      = regexp.MustCompile(`"([^"\\]+)"\s*:\s*(?:"((?:[^"\\]|\\.)*)|([^\s,{}\[\]"][^\s,{}\[\]"]*))`)
	jsonContainerRE = regexp.MustCompile(`"([^"\\]+)"\s*:\s*([{\[])`)
	encodedValueRE  = regexp.MustCompile(`=([^&;#\s]*%[0-9A-Fa-f]{2}[^&;#\s]*)`)
)

// maskEncodedValues masks each "=value" of c that percent-decodes into
// something secret ("a=1%26token%3Dx", "next=%2Fx%3Ftoken%3Dx").
func maskEncodedValues(c string, depth int) string {
	if depth <= 0 || !strings.Contains(c, "%") {
		return c
	}
	return encodedValueRE.ReplaceAllStringFunc(c, func(m string) string {
		if hasSecret(lenientUnescape(m[1:]), depth) {
			return "=" + Placeholder
		}
		return m
	})
}

// hasSecret reports whether d, a decoded parameter value, holds a masked URL,
// query pair or (decoding further) an encoded one.
func hasSecret(d string, depth int) bool {
	if depth <= 0 {
		return false
	}
	if maskSecretPairs("?"+d) != "?"+d || text(d, depth-1) != d {
		return true
	}
	return strings.Contains(d, "%") && hasSecret(lenientUnescape(d), depth-1)
}

// Body masks an API error body. Whole JSON (one value, nothing after it) is
// rewritten token by token, order and duplicate keys kept: Text on every key,
// Body again on a string that is itself JSON (a few levels), and any scalar
// under a secret-named key masked ("code" excepted, see isSecretField).
// Anything else, trailing junk and truncation included, goes through Text.
// Output is the input byte for byte when nothing was masked, else compact JSON.
func Body(s string) string { return body(s, maxEscapeLevels) }

func body(s string, depth int) string {
	t := strings.TrimSpace(s)
	if t == "" || !strings.ContainsAny(t[:1], `{["`) || !json.Valid([]byte(t)) {
		return Text(s)
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	var buf bytes.Buffer
	changed := false
	if rewriteJSON(dec, &buf, false, depth, &changed) != nil {
		return Text(s)
	}
	if !changed {
		return s
	}
	return buf.String()
}

func writeJSONString(buf *bytes.Buffer, s string) {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	buf.Truncate(buf.Len() - 1) // Encode's newline
}

// rewriteJSON copies the next value of dec to buf, masking as Body describes;
// secret says the value sits under a secret-named key.
func rewriteJSON(dec *json.Decoder, buf *bytes.Buffer, secret bool, depth int, changed *bool) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		closer := byte(']')
		if v == '{' {
			closer = '}'
		}
		buf.WriteString(v.String())
		for first := true; dec.More(); first = false {
			if !first {
				buf.WriteByte(',')
			}
			childSecret := secret
			if v == '{' {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				key, _ := kt.(string)
				nk := Text(key)
				*changed = *changed || nk != key
				writeJSONString(buf, nk)
				buf.WriteByte(':')
				childSecret = secret || isSecretField(key)
			}
			if err := rewriteJSON(dec, buf, childSecret, depth, changed); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
		buf.WriteByte(closer)
	case string:
		var out string
		if depth > 0 {
			out = body(v, depth-1)
		} else {
			out = Text(v)
		}
		if secret && v != "" {
			out = Placeholder
		}
		*changed = *changed || out != v
		writeJSONString(buf, out)
	case json.Number, bool:
		if secret {
			*changed = true
			writeJSONString(buf, Placeholder)
		} else if n, ok := v.(json.Number); ok {
			buf.WriteString(n.String())
		} else {
			buf.WriteString(strconv.FormatBool(v.(bool)))
		}
	default:
		buf.WriteString("null")
	}
	return nil
}
