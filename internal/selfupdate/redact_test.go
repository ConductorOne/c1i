package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/transport"
)

// A presigned URL from the manifest must not reach an error message.
func TestErrorsRedactPresignedURLs(t *testing.T) {
	const base = "https://dist.example"
	const signed = base + "/a/c1i.tar.gz?X-Amz-Signature=SEKS1&X-Amz-Credential=SEKS2&keep=1"
	check := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: want an error", name)
			return
		}
		if got := err.Error(); strings.Contains(got, "SEKS") || !strings.Contains(got, "keep=1") {
			t.Errorf("%s: %q", name, got)
		}
	}
	html := &transport.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: []byte("x")}
	check("validate off-host", (&Client{BaseURL: base}).validateURL("https://evil.example/c1i?X-Amz-Signature=SEKS1&keep=1"))
	check("validate non-https", (&Client{BaseURL: base}).validateURL("http://dist.example/c1i?X-Amz-Signature=SEKS1&keep=1"))
	check("validate unparsable", (&Client{BaseURL: base}).validateURL("https://dist.example/c1i?X-Amz-Signature=SEKS1&keep=1\x7f"))
	_, _, err := (&Client{BaseURL: base, HTTP: &fakeDoer{err: errors.New("boom")}}).ManifestRaw(context.Background(), signed)
	check("fetch failure", err)
	_, _, err = (&Client{BaseURL: base, HTTP: &fakeDoer{resp: map[string]*transport.Response{signed: jsonResp("{")}}}).ManifestRaw(context.Background(), signed)
	check("parse", err)
	_, _, err = (&Client{BaseURL: base, HTTP: &fakeDoer{resp: map[string]*transport.Response{signed: html}}}).ManifestRaw(context.Background(), signed)
	check("content type", err)
}

// The oversize-download and getJSON parse errors echo the URL too.
func TestDownloadAndParseErrorsRedactURL(t *testing.T) {
	const signed = "https://dist.example/a/c1i.tar.gz?X-Amz-Signature=SEKS1&keep=1"
	c := &Client{BaseURL: "https://dist.example", HTTP: &fakeDoer{resp: map[string]*transport.Response{
		signed: {StatusCode: 200, Body: make([]byte, MaxArtifactBytes+1)},
	}}}
	_, err := c.download(context.Background(), signed)
	if err == nil || strings.Contains(err.Error(), "SEKS1") || !strings.Contains(err.Error(), "keep=1") {
		t.Errorf("oversize: %v", err)
	}
	c = &Client{BaseURL: "https://dist.example", HTTP: &fakeDoer{resp: map[string]*transport.Response{signed: jsonResp("{")}}}
	var out map[string]any
	err = c.getJSON(context.Background(), signed, &out)
	if err == nil || strings.Contains(err.Error(), "SEKS1") || !strings.Contains(err.Error(), "keep=1") {
		t.Errorf("getJSON parse: %v", err)
	}
}
