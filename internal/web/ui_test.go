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

func TestManufacturingSectionIsAvailableToAdminAndProduction(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{
		"administrador:['raw-materials','finished-goods','packaging','logistics','billing','foxpro','audit','manufacturing']",
		"produccion:['raw-materials','finished-goods','packaging','logistics','manufacturing']",
		"'manufacturing':'Fabricación'",
		"'manufacturing':refreshManufacturing",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected manufacturing section to contain %q", want)
		}
	}
}

func TestManufacturingAdminControlsAreRoleGatedAndValidated(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{
		"function manufacturingAdminForms(){return activeRole==='administrador'?formOpeningStock()+formBOM():''}",
		`<input name="materialId" value="rm-100" placeholder="rm-100" required>`,
		`<input name="unit" value="m2" placeholder="m2" required>`,
		`<input name="quantity" type="number" value="100" min="1" step="1" required`,
		`<input name="id" value="bom-loafer-v1" placeholder="bom-loafer-v1" required>`,
		`<input name="revision" type="number" value="1" min="1" step="1" required`,
		`<input name="variantId" value="fg-loafer-negro-39" placeholder="fg-loafer-negro-39" required>`,
		`<input name="quantityPerUnit" type="number" value="1" min="1" step="1" required`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected manufacturing controls to contain %q", want)
		}
	}
}

func TestManufacturingActionsUseExpectedAPIsAndRefresh(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{
		`api('/api/manufacturing/materials/'+encodeURIComponent(materialId)+'/balance')`,
		`const materials=await api('/api/raw-materials')`,
		`document.getElementById('manufacturing-balance').textContent='Saldo disponible: '+balance.onHand+' '+context+' ('+balance.materialId+')'`,
		`api('/api/manufacturing/material-stock/openings',{method:'POST',body:JSON.stringify({materialId,unit,quantity})})`,
		`api('/api/manufacturing/boms',{method:'POST',body:JSON.stringify({id,revision,variantId,materialId,quantityPerUnit,unit})})`,
		`api('/api/manufacturing/work-orders',{method:'POST',body:JSON.stringify({id,bomId,plannedQty})})`,
		`api('/api/manufacturing/work-orders/'+encodeURIComponent(id)+'/release',{method:'POST'})`,
		`api('/api/manufacturing/work-orders/'+encodeURIComponent(id)+'/complete',{method:'POST',body:JSON.stringify({goodQty})})`,
		`<input name="plannedQty" type="number" value="1" min="1" step="1" required`,
		`<input name="goodQty" type="number" min="1" step="1" required`,
		`qty.max=String(order.plannedQty)`,
		`Number.isInteger(goodQty)||goodQty<=0||goodQty>order.plannedQty`,
		"await refreshManufacturing()",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected manufacturing action to contain %q", want)
		}
	}
	for _, function := range []string{"openMaterialStock", "saveBOM", "createWorkOrder", "releaseWorkOrder", "completeWorkOrder"} {
		start := strings.Index(html, "async function "+function+"(")
		if start < 0 {
			t.Fatalf("expected rendered html to contain %s action", function)
		}
		end := strings.Index(html[start+1:], "\nasync function ")
		if end < 0 {
			end = len(html) - start - 1
		}
		declaration := html[start : start+1+end]
		apiCall := strings.Index(declaration, "await api(")
		refreshCall := strings.Index(declaration, "await refreshManufacturing()")
		if apiCall < 0 || refreshCall <= apiCall {
			t.Fatalf("expected %s to refresh lists after its API call", function)
		}
		if !strings.Contains(declaration, "catch(error){showManufacturingError(error)}") {
			t.Fatalf("expected %s failures to appear in the manufacturing alert", function)
		}
	}
}

func TestManufacturingFailuresAreVisibleAndSuccessRefreshesLists(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{
		`<p id="manufacturing-alert" class="muted" role="alert" hidden></p>`,
		`function showManufacturingError(error){const alert=document.getElementById('manufacturing-alert');if(alert){alert.textContent=error&&error.message?error.message:'No se pudo completar la acción de fabricación';alert.hidden=false}}`,
		"typeof data==='string'?data:(data&&(data.error||JSON.stringify(data)))||res.statusText",
		"catch(error){showManufacturingError(error)}",
		"await refreshManufacturing()",
		"async function refreshManufacturing()",
		"manufacturingWorkOrders=workOrders",
		`catch(error){showManufacturingError(error)}}`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected visible manufacturing feedback to contain %q", want)
		}
	}
}

func TestManufacturingTablesUseSpanishLabelsForBothRoles(t *testing.T) {
	html := NewUI().Render()
	for _, want := range []string{
		"const boms=await api('/api/manufacturing/boms')",
		"'<h3>Listas de materiales</h3>'",
		"bomId:'Lista de materiales'",
		"plannedQty:'Cantidad planificada'",
		"goodQty:'Cantidad buena'",
		"variantId:'Variante'",
		"quantityPerUnit:'Cantidad por unidad'",
		"materialId:'Material'",
		"escapeHtml(colLabels[c]||c)",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected manufacturing tables to contain %q", want)
		}
	}
	start := strings.Index(html, "async function refreshManufacturing(){")
	if start < 0 {
		t.Fatal("expected rendered html to contain refreshManufacturing declaration")
	}
	end := strings.Index(html[start:], "\nfunction formRaw(){")
	if end < 0 {
		t.Fatal("expected refreshManufacturing declaration to end before formRaw")
	}
	refreshDecl := html[start : start+end]
	if strings.Contains(refreshDecl, "activeRole==='administrador'?await api('/api/manufacturing/boms')") {
		t.Fatal("expected both roles to read the materials list instead of hiding it from production")
	}
}

func TestManufacturingMutationsDoNotSendActorOrConsumption(t *testing.T) {
	html := NewUI().Render()
	for _, function := range []string{"openMaterialStock", "saveBOM", "createWorkOrder", "releaseWorkOrder", "completeWorkOrder"} {
		start := strings.Index(html, "async function "+function+"(")
		if start < 0 {
			t.Fatalf("expected rendered html to contain %s action", function)
		}
		lineEnd := strings.Index(html[start:], "\n")
		if lineEnd < 0 {
			t.Fatalf("expected %s declaration to end with a newline", function)
		}
		declaration := html[start : start+lineEnd]
		if !strings.HasSuffix(declaration, "}") {
			t.Fatalf("expected %s to stay declared on one line so this check cannot pass vacuously", function)
		}
		if strings.Contains(declaration, "actor") || strings.Contains(declaration, "consumption") {
			t.Fatalf("expected %s to leave actor and consumption to the server", function)
		}
	}
}
