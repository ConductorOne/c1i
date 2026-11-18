package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Rows keep real JSON types, emit null rather than "" for absence, and never
// carry a credential's secret.
func TestBridgeRows(t *testing.T) {
	decode := func(t *testing.T, raw string, v any) {
		t.Helper()
		if err := json.Unmarshal([]byte(raw), v); err != nil {
			t.Fatal(err)
		}
	}
	var never, seen bridge
	decode(t, `{"id":"br-1","displayName":"zz","description":"","appliance":null}`, &never)
	decode(t, `{"id":"br-1","displayName":"zz","appliance":{"status":"TUNNEL_APPLIANCE_STATUS_CONNECTED","lastSeenAt":"2026-01-01T00:00:00Z","announcedServiceCount":2}}`, &seen)
	var cred bridgeCredential
	decode(t, `{"id":"word-slug-1","bridgeId":"br-1","clientId":"cid","clientSecret":"","credentialStatus":"TUNNEL_CREDENTIAL_STATUS_REVOKED","expiresTime":null}`, &cred)

	for name, tc := range map[string]struct {
		got, want map[string]any
	}{
		"bridge without appliance": {bridgeRow(never), map[string]any{
			"id": "br-1", "display_name": "zz", "description": nil, "appliance_status": nil, "last_seen_at": nil,
			"announced_service_count": nil, "created_at": nil, "updated_at": nil}},
		"bridge with appliance": {bridgeRow(seen), map[string]any{
			"id": "br-1", "display_name": "zz", "description": nil, "appliance_status": "TUNNEL_APPLIANCE_STATUS_CONNECTED",
			"last_seen_at": "2026-01-01T00:00:00Z", "announced_service_count": 2, "created_at": nil, "updated_at": nil}},
		"credential": {bridgeCredentialRow(cred), map[string]any{
			"id": "word-slug-1", "bridge_id": "br-1", "client_id": "cid", "credential_status": "TUNNEL_CREDENTIAL_STATUS_REVOKED",
			"created_at": nil, "expires_time": nil, "last_used_at": nil, "revoked_at": nil}},
		"announced service": {announcedServiceRow(map[string]any{"name": "db", "port": float64(5432), "serviceType": "", "transportType": "tcp"}),
			map[string]any{"name": "db", "port": float64(5432), "service_type": nil, "service_path": nil, "transport_type": "tcp"}},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s: row = %#v, want %#v", name, tc.got, tc.want)
		}
	}
}

func TestBridgesServicesListsAnnouncedServices(t *testing.T) {
	s := newMutServer(t, fixedReply(200, `{"list":[{"name":"db","port":5432,"serviceType":"postgres","servicePath":"","transportType":"tcp"}]}`))
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, nil, "bridges", "services", "br-1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	if len(s.seen) != 1 || s.seen[0].method != "GET" || s.seen[0].rawPath != "/api/v1/iam/tunnel/bridges/br-1/announced_services" {
		t.Errorf("requests = %v", s.seen)
	}
	want := `{"name":"db","port":5432,"service_path":null,"service_type":"postgres","transport_type":"tcp"}`
	if got := strings.TrimSpace(r.stdout); got != want {
		t.Errorf("stdout = %s, want %s", got, want)
	}
}
