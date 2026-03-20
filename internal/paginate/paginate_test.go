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

func TestSliceArray(t *testing.T) {
	input := `[1,2,3,4,5,6,7,8,9,10]`

	tests := []struct {
		name       string
		offset     int
		limit      int
		wantTotal  int
		wantOffset int
		wantLimit  int
	}{
		{"first page", 0, 3, 10, 0, 3},
		{"middle page", 3, 3, 10, 3, 3},
		{"last page partial", 8, 3, 10, 8, 3},
		{"offset beyond end", 20, 3, 10, 20, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SliceArray(
				input,
				Params{Active: true, Offset: tt.offset, Limit: tt.limit},
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Total != tt.wantTotal {
				t.Errorf("total: got %d, want %d", result.Total, tt.wantTotal)
			}
			if result.Offset != tt.wantOffset {
				t.Errorf(
					"offset: got %d, want %d",
					result.Offset,
					tt.wantOffset,
				)
			}
			if result.Limit != tt.wantLimit {
				t.Errorf("limit: got %d, want %d", result.Limit, tt.wantLimit)
			}
		})
	}
}

func TestSliceArrayNotArray(t *testing.T) {
	input := `{"key": "value"}`
	_, err := SliceArray(input, Params{Active: true, Offset: 0, Limit: 10})
	if err != ErrNotArray {
		t.Errorf("expected ErrNotArray, got %v", err)
	}
}

func TestSliceArrayInactiveParams(t *testing.T) {
	_, err := SliceArray(`[1,2,3]`, Params{Active: false})
	if err != ErrNotActive {
		t.Errorf("expected ErrNotActive, got %v", err)
	}
}
