package paginate

import "testing"

func TestParseParams(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantClean string
		wantOff   int
		wantLim   int
		wantOk    bool
	}{
		{"no params", "caldav://tasks", "caldav://tasks", 0, 0, false},
		{
			"offset only",
			"caldav://tasks?offset=10",
			"caldav://tasks",
			10,
			50,
			true,
		},
		{
			"both",
			"caldav://tasks?offset=0&limit=25",
			"caldav://tasks",
			0,
			25,
			true,
		},
		{
			"limit only ignored",
			"caldav://tasks?limit=25",
			"caldav://tasks",
			0,
			0,
			false,
		},
		{
			"preserves other params",
			"caldav://tasks?foo=bar&offset=5&limit=10",
			"caldav://tasks?foo=bar",
			5,
			10,
			true,
		},
		{
			"offset zero",
			"caldav://tasks?offset=0",
			"caldav://tasks",
			0,
			50,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clean, params := ParseParams(tt.uri)
			if clean != tt.wantClean {
				t.Errorf("clean URI: got %q, want %q", clean, tt.wantClean)
			}
			if params.Active != tt.wantOk {
				t.Errorf("active: got %v, want %v", params.Active, tt.wantOk)
			}
			if params.Active {
				if params.Offset != tt.wantOff {
					t.Errorf(
						"offset: got %d, want %d",
						params.Offset,
						tt.wantOff,
					)
				}
				if params.Limit != tt.wantLim {
					t.Errorf("limit: got %d, want %d", params.Limit, tt.wantLim)
				}
			}
		})
	}
}
