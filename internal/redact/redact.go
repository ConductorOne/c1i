// Package redact masks credentials in URLs. It is the one rule behind --dry-run
// previews and --debug / error output, so they never disagree.
package redact

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Placeholder replaces every masked value.
const Placeholder = "[redacted]"

// FoldKey folds a JSON key's camelCase, snake_case and kebab-case spellings
// (the API accepts the first two) into one lookup form.
func FoldKey(k string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(k))
}

var (
	// A folded name is secret when it ends with one of these...
	secretSuffixes = []string{"token", "secret", "secretkey", "password", "passphrase", "signature", "apikey", "privatekey", "accesskey", "signingkey", "encryptionkey", "sshkey", "credential", "credentials", "bearer"}
	// ...or equals one of these (names the suffixes miss, incl. the MCP server
	// builders' headerValue/credentialsJson/secretAccessKey; cmd pins that).
	secretExact = map[string]bool{
		"auth": true, "authorization": true, "code": true, "sig": true,
		"pwd": true, "pass": true, "otp": true, "jwt": true, "hmac": true, "cookie": true, "setcookie": true,
		"connectionstring": true, "dsn": true,
		"headervalue": true, "credentialsjson": true, "secretaccesskey": true,
	}
)

// IsSecretKey is the one rule for a credential-shaped name, shared by body
// fields and URL parameters. It is best-effort by name; tokenType, secretName
// and author stay visible because only suffixes and exact names count.
func IsSecretKey(k string) bool {
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

// isSecretParam adds the names that are secret only as a URL parameter: a
// body field called "key" is an ordinary field.
func isSecretParam(k string) bool { return IsSecretKey(k) || FoldKey(k) == "key" }

// URL masks a URL's userinfo (the password, or the username when there
// is none: `https://<token>@host` is the common PAT form; the conventional
// non-secret "git" user is kept) and the value of every secret-named query,
// fragment or `;` parameter, leaving the rest as sent. A string that does not
// parse as a URL with a host goes through urlText instead, so a
// malformed or scheme-less one fails closed.
func URL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return urlText(s)
	}
	if u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.UserPassword(u.User.Username(), Placeholder)
		} else if u.User.Username() != gitUser {
			u.User = url.User(Placeholder)
		}
		s = strings.Replace(u.String(), "%5Bredacted%5D", Placeholder, 1)
	}
	return maskSecretPairs(s)
}

const gitUser = "git"

var (
	urlPasswordRE  = regexp.MustCompile(`(//[^/?#:@]*:)[^/?#]*@`)
	urlUsernameRE  = regexp.MustCompile(`(//)([^/?#:@]+)@`)
	bareUserinfoRE = regexp.MustCompile(`^([^/@\s:]+:)[^/@\s]*@`)
	bareTokenRE    = regexp.MustCompile(`^([^/@\s:]+)@([^/@\s]*\.[^/@\s]*)`)
	pathParamRE    = regexp.MustCompile(`;([^=;/?#\s]+)=([^;/?#\s]*)`)
)

// urlText is URL's fail-closed fallback for text url.Parse cannot
// use: it masks userinfo (a bare "TOKEN@host.tld/..." too, which also hits an
// email-shaped string in URL position) and the secret-named parameters.
func urlText(s string) string {
	s = urlPasswordRE.ReplaceAllString(s, "${1}"+Placeholder+"@")
	s = urlUsernameRE.ReplaceAllStringFunc(s, func(m string) string {
		if m == "//"+gitUser+"@" {
			return m
		}
		return "//" + Placeholder + "@"
	})
	s = bareUserinfoRE.ReplaceAllString(s, "${1}"+Placeholder+"@")
	s = bareTokenRE.ReplaceAllStringFunc(s, func(m string) string {
		user, host, _ := strings.Cut(m, "@")
		if user == gitUser {
			return m
		}
		return Placeholder + "@" + host
	})
	return maskSecretPairs(s)
}

// maskSecretPairs masks secret-named parameters. In a query only "&" ends a
// pair, so a ";", "#" or "?" inside a secret value cannot leave part of it
// showing; a fragment's pairs end at "&" or ";"; path ";k=v" parameters end at
// "/". Pairs are also tried percent-decoded ("token%3Dx"). The rest is as sent.
func maskSecretPairs(s string) string {
	end := strings.IndexAny(s, "?#")
	head, tail := s, ""
	if end >= 0 {
		head, tail = s[:end], s[end:]
	}
	head = pathParamRE.ReplaceAllStringFunc(head, func(m string) string {
		if k, _, _ := strings.Cut(m[1:], "="); isSecretParam(k) {
			return ";" + k + "=" + Placeholder
		}
		return m
	})
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
			if h, frag, ok := strings.Cut(p, "#"); ok {
				pieces[i] = maskSemi(h) + "#" + maskSemi(frag)
				continue
			}
		}
		pieces[i] = maskSemi(p)
	}
	return head + tail[:1] + strings.Join(pieces, "&")
}

// maskSemi masks each ";"-separated pair of seg.
func maskSemi(seg string) string {
	parts := strings.Split(seg, ";")
	for i, p := range parts {
		parts[i] = maskPair(p)
	}
	return strings.Join(parts, ";")
}

// maskPair masks the whole of "k=v" when k, percent-decoded, is secret.
func maskPair(p string) string {
	d := p
	if u, err := url.QueryUnescape(p); err == nil {
		d = u
	}
	if k, _, ok := strings.Cut(d, "="); ok && isSecretParam(k) {
		return k + "=" + Placeholder
	}
	return p
}

// ErrorURL masks the request URL inside a *url.Error in place, so its text no
// longer carries a secret; the type and wrap chain are unchanged. Call it only
// on an error you own (one fresh from net/http).
func ErrorURL(err error) {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = URL(ue.URL)
	}
}
