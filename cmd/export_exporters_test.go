package cmd

import (
	"net/http"
	"strings"
	"testing"
)

const exporterEnv = `{"exporter":{"exportId":"ex-1","displayName":"old","state":"EXPORT_STATE_WAITING","watermarkEventId":"w","datasource":{"datasourceId":"ds-1","prefix":"p/","format":"EXPORT_FORMAT_OCSF_JSON_GZIP"}}}`

func TestExporterEventsReadsExporterFirstAndPages(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	// An unknown id: the exporter read 404s; the events POST (an empty 200 on
	// the real API) must never be sent.
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{status: 404, body: `{"code":5}`}
		}
		return mutReply{body: `{"list":[],"nextPageToken":""}`}
	})
	r := runListCLI(t, s.srv, "export", "exporters", "events", "ex-9")
	if r.exit != exitNotFound || r.stdout != "" {
		t.Errorf("exit %d stdout %q, want 4 and nothing", r.exit, r.stdout)
	}
	for _, q := range s.requests() {
		if q.method == http.MethodPost {
			t.Errorf("sent %s %s for an unknown exporter", q.method, q.rawPath)
		}
	}

	// A known id pages to the end, one NDJSON event per line.
	s = newMutServer(t, func(r *http.Request, n int) mutReply {
		switch {
		case r.Method == http.MethodGet:
			return mutReply{body: exporterEnv}
		case n == 2:
			return mutReply{body: `{"list":[{"uid":"e1"},{"uid":"e2"}],"nextPageToken":"t2"}`}
		default:
			return mutReply{body: `{"list":[{"uid":"e3"}],"nextPageToken":""}`}
		}
	})
	r = runListCLI(t, s.srv, "export", "exporters", "events", "ex-1")
	if want := "{\"uid\":\"e1\"}\n{\"uid\":\"e2\"}\n{\"uid\":\"e3\"}\n"; r.err != nil || r.stdout != want {
		t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
	}
	if got := s.requests()[1]; got.method != http.MethodPost || got.rawPath != "/api/v1/systemlog/exports/ex-1/events" {
		t.Errorf("events request = %+v", got)
	}
	if got := s.requests()[2]; !strings.Contains(got.body, `"pageToken":"t2"`) {
		t.Errorf("second page body = %s, want the cursor", got.body)
	}
}

func TestExporterUpdateSendsDatasourceAndMask(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"prefix", []string{"--prefix", "n/"},
			`{"exporter":{"datasource":{"datasourceId":"ds-1","format":"EXPORT_FORMAT_OCSF_JSON_GZIP","prefix":"n/"},"displayName":"old"},"updateMask":"datasource.prefix"}`},
		{"name and format", []string{"--display-name", "new", "--format", "zstd"},
			`{"exporter":{"datasource":{"datasourceId":"ds-1","format":"EXPORT_FORMAT_OCSF_JSON_ZSTD","prefix":"p/"},"displayName":"new"},"updateMask":"displayName,datasource.format"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			for _, dry := range []bool{false, true} {
				if dry {
					withDryRun(t)
				}
				s := newMutServer(t, fixedReply(200, exporterEnv))
				r := runMutationCLI(t, s.srv, 0, nil, append([]string{"export", "exporters", "update", "ex-1"}, tc.args...)...)
				if r.err != nil {
					t.Fatalf("dry=%v: exit %d: %v", dry, r.exit, r.err)
				}
				reqs := s.requests()
				if reqs[0].method != http.MethodGet || reqs[0].rawPath != "/api/v1/systemlog/exports/ex-1" {
					t.Errorf("dry=%v: first request %+v, want the exporter read", dry, reqs[0])
				}
				if dry {
					if len(reqs) != 1 {
						t.Errorf("preview sent %+v, want only the read", reqs)
					}
					last := previewDoc(t, r.stdout)[0]
					if got := compactJSON(t, last["body"]); got != tc.want {
						t.Errorf("preview body = %s\nwant         %s", got, tc.want)
					}
					continue
				}
				if got := compactRaw(t, reqs[1].body); got != tc.want {
					t.Errorf("body = %s\nwant   %s", got, tc.want)
				}
			}
		})
	}
}

func TestExporterUpdateUsageErrors(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, args := range [][]string{
		{"export", "exporters", "update", "ex-1"},
		{"export", "exporters", "update", "ex-1", "--format", "bogus"},
		{"export", "exporters", "update", "ex-1", "--prefix", " "},
	} {
		s := newMutServer(t, fixedReply(200, exporterEnv))
		r := runMutationCLI(t, s.srv, 0, nil, args...)
		if r.exit != exitUsage {
			t.Errorf("%v: exit %d, want 2", args, r.exit)
		}
		for _, q := range s.requests() {
			if q.method != http.MethodGet {
				t.Errorf("%v: sent %s", args, q.method)
			}
		}
	}
}

func TestExporterUpdateReadBackWithoutDatasourceExits6(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"exporter":{"exportId":"ex-1"}}`))
	r := runMutationCLI(t, s.srv, 0, nil, "export", "exporters", "update", "ex-1", "--prefix", "x/")
	if r.exit != exitServer {
		t.Errorf("exit %d, want 6", r.exit)
	}
	for _, q := range s.requests() {
		if q.method == http.MethodPost {
			t.Errorf("wrote after an unusable read-back: %+v", q)
		}
	}
}

func TestExporterCreateBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, exporterEnv))
	r := runMutationCLI(t, s.srv, 0, nil, "export", "exporters", "create", "--display-name", "zz", "--datasource-id", "ds-1", "--prefix", "zz/", "--format", "gzip")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	want := `{"datasource":{"datasourceId":"ds-1","format":"EXPORT_FORMAT_OCSF_JSON_GZIP","prefix":"zz/"},"displayName":"zz"}`
	if got := compactRaw(t, s.requests()[0].body); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	// Omitted prefix/format are omitted, not sent empty.
	s = newMutServer(t, fixedReply(200, exporterEnv))
	runMutationCLI(t, s.srv, 0, nil, "export", "exporters", "create", "--display-name", "zz", "--datasource-id", "ds-1")
	if got, want := compactRaw(t, s.requests()[0].body), `{"datasource":{"datasourceId":"ds-1"},"displayName":"zz"}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	r = runMutationCLI(t, nil, 0, nil, "export", "exporters", "create", "--display-name", "zz", "--datasource-id", "ds-1", "--format", "bogus")
	if r.exit != exitUsage {
		t.Errorf("bogus format: exit %d, want 2", r.exit)
	}
}

func TestExporterSearchBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"list":[{"exportId":"ex-1"}],"nextPageToken":""}`))
	r := runListCLI(t, s.srv, "export", "exporters", "search", "--query", "q", "--export-id", "a", "--export-id", "b")
	if r.err != nil || r.stdout != "{\"exportId\":\"ex-1\"}\n" {
		t.Fatalf("exit %d stdout %q: %v", r.exit, r.stdout, r.err)
	}
	got := s.requests()[0]
	if got.rawPath != "/api/v1/search/systemlog/exports" || !strings.Contains(got.body, `"refs":[{"exportId":"a"},{"exportId":"b"}]`) || !strings.Contains(got.body, `"query":"q"`) {
		t.Errorf("request = %+v", got)
	}
	r = runListCLI(t, nil, "export", "exporters", "search", "--export-id", " ")
	if r.exit != exitUsage {
		t.Errorf("blank --export-id: exit %d, want 2", r.exit)
	}
}

func TestExporterCreateUnknownDatasourceExits4(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	args := []string{"export", "exporters", "create", "--display-name", "zz", "--datasource-id", "ds-9", "--error-format", "json"}
	s := newMutServer(t, fixedReply(400, `{"code":3,"message":"datasource not found"}`))
	r := runMutationCLI(t, s.srv, 0, nil, args...)
	if r.exit != exitNotFound || r.errorObject(t)["applied"] != false {
		t.Errorf("exit %d, error %v; want 4 with applied:false", r.exit, r.errorObject(t))
	}
	s = newMutServer(t, fixedReply(400, `{"code":3,"message":"invalid ExportServiceCreateRequest.ExportTo: value is required"}`))
	if r := runMutationCLI(t, s.srv, 0, nil, args...); r.exit != exitUsage {
		t.Errorf("other 400: exit %d, want 2", r.exit)
	}
	s = newMutServer(t, fixedReply(404, `{"code":5,"message":"datasource not found"}`))
	if r := runMutationCLI(t, s.srv, 0, nil, args...); r.exit != exitNotFound {
		t.Errorf("404: exit %d, want 4", r.exit)
	}
}
