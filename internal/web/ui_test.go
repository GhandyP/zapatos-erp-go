package web

import (
	"strings"
	"testing"
)

func TestRenderIncludesDimensionsAreas(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{"Embalaje / transporte", "Stock y logística", "lengthCm", "widthCm", "heightCm", "weightKg"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected html to contain %q", want)
		}
	}
}
