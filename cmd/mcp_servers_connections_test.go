package cmd

import "testing"

// TestConnectionRowAbsentValuesAreNullNotEmptyString pins that connected_at,
// authorized_as_email, and authorized_as_name are untyped nil, not "", when
// the calling user hasn't connected — "" is truthy in jq, so
// `jq 'select(.connected_at)'` would otherwise match every row regardless
// of whether it's connected.
func TestConnectionRowAbsentValuesAreNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name              string
		connectedAt       string
		authorizedAsEmail string
		authorizedAsName  string
		wantConnectedAt   any
		wantEmail         any
		wantName          any
	}{
		{
			name:              "not connected",
			connectedAt:       "",
			authorizedAsEmail: "",
			authorizedAsName:  "",
			wantConnectedAt:   nil,
			wantEmail:         nil,
			wantName:          nil,
		},
		{
			name:              "connected",
			connectedAt:       "2026-01-02T03:04:05Z",
			authorizedAsEmail: "user@example.com",
			authorizedAsName:  "Ada Lovelace",
			wantConnectedAt:   "2026-01-02T03:04:05Z",
			wantEmail:         "user@example.com",
			wantName:          "Ada Lovelace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := connectionRow(connectionView{
				ConnectorID:       "c1",
				ConnectedAt:       tt.connectedAt,
				AuthorizedAsEmail: tt.authorizedAsEmail,
				AuthorizedAsName:  tt.authorizedAsName,
			})

			gotConnectedAt, ok := row["connected_at"]
			if !ok {
				t.Fatal("row has no connected_at key")
			}
			if tt.wantConnectedAt == nil {
				if gotConnectedAt != nil {
					t.Errorf("connected_at = %#v, want untyped nil", gotConnectedAt)
				}
			} else if gotConnectedAt != tt.wantConnectedAt {
				t.Errorf("connected_at = %v, want %v", gotConnectedAt, tt.wantConnectedAt)
			}

			gotEmail, ok := row["authorized_as_email"]
			if !ok {
				t.Fatal("row has no authorized_as_email key")
			}
			if tt.wantEmail == nil {
				if gotEmail != nil {
					t.Errorf("authorized_as_email = %#v, want untyped nil", gotEmail)
				}
			} else if gotEmail != tt.wantEmail {
				t.Errorf("authorized_as_email = %v, want %v", gotEmail, tt.wantEmail)
			}

			gotName, ok := row["authorized_as_name"]
			if !ok {
				t.Fatal("row has no authorized_as_name key")
			}
			if tt.wantName == nil {
				if gotName != nil {
					t.Errorf("authorized_as_name = %#v, want untyped nil", gotName)
				}
			} else if gotName != tt.wantName {
				t.Errorf("authorized_as_name = %v, want %v", gotName, tt.wantName)
			}
		})
	}
}
