package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

// A --dry-run preview (CheckRequest) and a real send (Request) must refuse a
// bad path with the identical error.
func TestCheckRequestMatchesSendGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server reached for %s", r.URL.Path)
	}))
	defer srv.Close()
	c := newTestClient(srv, 0)
	for _, id := range []string{"", ".", "..", "..;x", "%2e"} {
		path := "/api/v1/apps/" + id
		_, sendErr := c.Request(context.Background(), http.MethodDelete, path, nil, nil)
		checkErr := CheckRequest(http.MethodDelete, srv.URL+path)
		var sendPE, checkPE *PathError
		if !errors.As(sendErr, &sendPE) || !errors.As(checkErr, &checkPE) || !reflect.DeepEqual(sendPE, checkPE) {
			t.Errorf("%q: CheckRequest = %v, send = %v; want the identical *PathError", id, checkErr, sendErr)
		}
	}
	if err := CheckRequest(http.MethodPost, "://bad"); err == nil {
		t.Error("CheckRequest accepted an unparseable URL")
	}
	if err := CheckRequest(http.MethodPost, srv.URL+"/api/v1/apps/a"); err != nil {
		t.Errorf("CheckRequest on a good URL = %v", err)
	}
}

// A retried attempt that reached the server is reported on the final APIError.
func TestAPIErrorCarriesPriorSent(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewForTesting(srv.URL, srv.Client(), WithMaxRetries(1))
	_, err := c.Delete(context.Background(), "/api/v1/apps/a")
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != 404 || !ae.PriorSent {
		t.Fatalf("err = %#v, want a 404 APIError with PriorSent", err)
	}
}
