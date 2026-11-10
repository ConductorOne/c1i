package envbool

import "testing"

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    bool
		wantErr bool
	}{
		{"1", true, false}, {"t", true, false}, {"T", true, false}, {"true", true, false}, {"TRUE", true, false},
		{"0", false, false}, {"f", false, false}, {"false", false, false},
		{"yes", false, true}, {"", false, true}, {"garbage", false, true},
	} {
		got, err := Parse(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("Parse(%q) = %v, %v; want %v, err %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}
