package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// fakeTokenSource is a controllable oauth2.TokenSource for exercising
// mintWithContext without network or credentials.
type fakeTokenSource struct {
	tok     *oauth2.Token
	err     error
	release chan struct{} // if non-nil, Token blocks until it is closed
}

func (f *fakeTokenSource) Token() (*oauth2.Token, error) {
	if f.release != nil {
		<-f.release
	}
	return f.tok, f.err
}

// TestMintWithContextSuccess: a ready source returns its token verbatim.
func TestMintWithContextSuccess(t *testing.T) {
	want := &oauth2.Token{AccessToken: "abc"}
	got, err := mintWithContext(context.Background(), &fakeTokenSource{tok: want})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AccessToken != "abc" {
		t.Errorf("token = %q, want abc", got.AccessToken)
	}
}

// TestMintWithContextErrorIsAuthError: a mint failure is wrapped as AuthError so
// it classifies to exit 3.
func TestMintWithContextErrorIsAuthError(t *testing.T) {
	_, err := mintWithContext(context.Background(), &fakeTokenSource{err: errors.New("boom")})
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Errorf("error %v is not an *AuthError", err)
	}
}

// TestMintWithContextCancellation: when ctx is already done, mint returns the
// context error promptly (not an AuthError) even though the source is still
// blocked, and the goroutine does not leak (buffered channel + released source).
func TestMintWithContextCancellation(t *testing.T) {
	release := make(chan struct{})
	defer close(release) // let the blocked goroutine finish after we return

	src := &fakeTokenSource{tok: &oauth2.Token{AccessToken: "x"}, release: release}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	done := make(chan struct{})
	var err error
	go func() {
		_, err = mintWithContext(ctx, src)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mintWithContext did not return promptly on canceled ctx")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		t.Error("cancellation should not be classified as an AuthError")
	}
}
