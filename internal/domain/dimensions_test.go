package domain

import "testing"

func TestDimensionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		d       Dimensions
		wantErr bool
	}{
		{name: "valid", d: Dimensions{LengthCM: 10, WidthCM: 5, HeightCM: 2, WeightKG: 0.4}},
		{name: "zero length", d: Dimensions{LengthCM: 0, WidthCM: 5, HeightCM: 2, WeightKG: 0.4}, wantErr: true},
		{name: "negative weight", d: Dimensions{LengthCM: 10, WidthCM: 5, HeightCM: 2, WeightKG: -1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.d.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
