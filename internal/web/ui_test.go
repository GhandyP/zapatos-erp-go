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

func TestRenderIncludesImprovedUXElements(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{
		"moduleNav",
		"role=\"status\"",
		"aria-live=\"polite\"",
		"Sin materia prima registrada",
		"deleteRaw",
		"Promise.all",
		"Centro de mando",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected html to contain %q", want)
		}
	}
}
