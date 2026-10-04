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

func TestRawMaterialFormRequiresValidDimensions(t *testing.T) {
	html := NewUI().Render()
	start := strings.Index(html, "function formRaw(){")
	if start < 0 {
		t.Fatal("expected rendered html to contain formRaw declaration")
	}
	end := strings.Index(html[start:], "\nfunction formFinished(){")
	if end < 0 {
		t.Fatal("expected rendered formRaw declaration to end before formFinished")
	}
	rawFormDecl := html[start : start+end]
	for _, want := range []string{
		`<label class="field">ID<input name="id" placeholder="Código" required></label>`,
		`<label class="field">Nombre<input name="name" placeholder="Nombre" required></label>`,
		`<label class="field">Unidad<input name="unit" placeholder="kg, m, unidad" required></label>`,
		`<label class="field">Stock mínimo<input name="minStock" type="number" min="0" step="1" required placeholder="0"></label>`,
		`<input name="lengthCm" type="number" step="0.01" min="0.01" required`,
		`<input name="widthCm" type="number" step="0.01" min="0.01" required`,
		`<input name="heightCm" type="number" step="0.01" min="0.01" required`,
		`<input name="weightKg" type="number" step="0.01" min="0" required`,
	} {
		if !strings.Contains(rawFormDecl, want) {
			t.Fatalf("expected raw-material form declaration to contain %q", want)
		}
	}
}

func TestRawMaterialSaveUsesSafeControlsAndHandlesFailures(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{
		`function rawFieldValue(form,name){const field=form.elements.namedItem(name);if(!field)throw new Error('Missing form field: '+name);return field.value}`,
		`function showRawError(form,error){const alert=form.querySelector('[role="alert"]');if(alert){alert.textContent=error&&error.message?error.message:'No se pudo guardar la materia prima';alert.hidden=false}}`,
		`catch(error){showRawError(form,error)}`,
		`payload={id:rawFieldValue(form,'id'),name:rawFieldValue(form,'name'),unit:rawFieldValue(form,'unit'),minStock:Number(rawFieldValue(form,'minStock'))`,
		`rawFieldValue(form,'minStock')`,
		`dimensions:{lengthCm:Number(rawFieldValue(form,'lengthCm')),widthCm:Number(rawFieldValue(form,'widthCm')),heightCm:Number(rawFieldValue(form,'heightCm')),weightKg:Number(rawFieldValue(form,'weightKg'))}`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected rendered html to contain %q", want)
		}
	}

	start := strings.Index(html, "async function saveRaw(form){")
	if start < 0 {
		t.Fatal("expected rendered html to contain saveRaw declaration")
	}
	end := strings.Index(html[start:], "\nasync function saveFinished(form)")
	if end < 0 {
		t.Fatal("expected rendered saveRaw declaration to end before saveFinished")
	}
	saveRawDecl := html[start : start+end]
	apiCall := strings.Index(saveRawDecl, "await api('/api/raw-materials'")
	refreshCall := strings.Index(saveRawDecl, "await refreshRaw()")
	if apiCall < 0 || refreshCall <= apiCall {
		t.Fatal("expected raw-material refresh to happen only after the save API resolves")
	}
}

func TestAuditoriaSeesRawMaterialsWithoutCreateForm(t *testing.T) {
	html := NewUI().Render()
	if !strings.Contains(html, "activeRole=me.role;") {
		t.Fatal("expected current role to be recorded for raw-material form access")
	}

	start := strings.Index(html, "async function refreshRaw(){")
	if start < 0 {
		t.Fatal("expected rendered html to contain refreshRaw declaration")
	}
	end := strings.Index(html[start:], "\nasync function refreshFinished()")
	if end < 0 {
		t.Fatal("expected rendered refreshRaw declaration to end before refreshFinished")
	}
	refreshRawDecl := html[start : start+end]
	for _, want := range []string{"activeRole==='auditoria'?'':formRaw()", "table(items,['id','name','unit','minStock','dimensions'])"} {
		if !strings.Contains(refreshRawDecl, want) {
			t.Fatalf("expected refreshRaw declaration to contain %q", want)
		}
	}
}
