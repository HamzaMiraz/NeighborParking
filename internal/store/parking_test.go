package store

import (
	"testing"

	"neighborparking/internal/domain"
	"neighborparking/internal/platform"
)

func TestValidateLayout(t *testing.T) {
	tests := []struct {
		name string
		in   domain.LayoutInput
		code string
	}{
		{"valid", domain.LayoutInput{Rows: 2, Cols: 2, Cells: []domain.MapCell{{Row: 0, Col: 0, Type: "PARKING", Label: "A-1"}, {Row: 0, Col: 1, Type: "ROAD"}}}, ""},
		{"invalid dimensions", domain.LayoutInput{Rows: 0, Cols: 2}, "INVALID_GRID_SIZE"},
		{"out of bounds", domain.LayoutInput{Rows: 2, Cols: 2, Cells: []domain.MapCell{{Row: 2, Col: 0, Type: "ROAD"}}}, "CELL_OUT_OF_BOUNDS"},
		{"duplicate position", domain.LayoutInput{Rows: 2, Cols: 2, Cells: []domain.MapCell{{Row: 0, Col: 0, Type: "ROAD"}, {Row: 0, Col: 0, Type: "WALL"}}}, "DUPLICATE_CELL"},
		{"missing slot name", domain.LayoutInput{Rows: 2, Cols: 2, Cells: []domain.MapCell{{Row: 0, Col: 0, Type: "PARKING"}}}, "SLOT_NAME_REQUIRED"},
		{"duplicate slot name", domain.LayoutInput{Rows: 2, Cols: 2, Cells: []domain.MapCell{{Row: 0, Col: 0, Type: "PARKING", Label: "A-1"}, {Row: 0, Col: 1, Type: "PARKING", Label: "a-1"}}}, "DUPLICATE_SLOT_NAME"},
		{"invalid type", domain.LayoutInput{Rows: 2, Cols: 2, Cells: []domain.MapCell{{Row: 0, Col: 0, Type: "SWIMMING_POOL"}}}, "INVALID_CELL_TYPE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLayout(tt.in)
			if tt.code == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.code != "" {
				if err == nil {
					t.Fatalf("expected %s", tt.code)
				}
				if got := platform.AsAppError(err).Code; got != tt.code {
					t.Fatalf("code = %s, want %s", got, tt.code)
				}
			}
		})
	}
}

func TestCommunityCodeUsesUnambiguousAlphabet(t *testing.T) {
	for i := 0; i < 100; i++ {
		code := CommunityCode()
		if len(code) != 8 {
			t.Fatalf("length = %d", len(code))
		}
		for _, forbidden := range "01IO" {
			for _, char := range code {
				if char == forbidden {
					t.Fatalf("code %q includes ambiguous character %q", code, char)
				}
			}
		}
	}
}
