package integration

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

// The Issuer freezes (#455, stories 4–6 of #450): the RUC cannot change once
// any Tax Invoice exists for the Issuer in either environment, because it is
// baked into every clave de acceso; establecimiento and punto de emisión
// cannot change once a sequence row exists under them, because numbering
// must stay continuous. Every other detail stays editable. The read carries
// `frozen_fields` so the page can grey the inputs and say why.

// frozenIssuerView is the Issuer read with the freeze flags.
type frozenIssuerView struct {
	ecuadorIssuerView
	FrozenFields []string `json:"frozen_fields"`
}

func getFrozenIssuer(t *testing.T, sessionID string) frozenIssuerView {
	t.Helper()
	resp, env := sriEnv.get(t, ecuadorIssuerPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET issuer status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view frozenIssuerView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode issuer: %v", err)
	}
	return view
}

func frozenFieldOf(t *testing.T, env envelope) string {
	t.Helper()
	raw, _ := json.Marshal(env.Error.Details)
	var details struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode details %s: %v", raw, err)
	}
	return details.Field
}

func expectFrozen(t *testing.T, what string, resp *http.Response, env envelope, field string) {
	t.Helper()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("%s status=%d error=%+v, want 409", what, resp.StatusCode, env.Error)
	}
	if env.Error == nil || env.Error.Code != "ISSUER_FIELD_FROZEN" {
		t.Fatalf("%s error=%+v, want ISSUER_FIELD_FROZEN", what, env.Error)
	}
	if got := frozenFieldOf(t, env); got != field {
		t.Fatalf("%s froze field %q, want %q", what, got, field)
	}
	if string(env.Data) != "null" {
		t.Fatalf("%s data=%s, want null", what, env.Data)
	}
}

// TestIssuerNothingFrozenBeforeIssuing: with no invoice and no sequence the
// read reports no frozen fields and the RUC, establecimiento and punto de
// emisión all change freely.
func TestIssuerNothingFrozenBeforeIssuing(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	putEcuadorIssuer(t, sriEnv, sessionID, validEcuadorIssuerBody())

	view := getFrozenIssuer(t, sessionID)
	if len(view.FrozenFields) != 0 {
		t.Fatalf("frozen_fields = %v before any issue, want none", view.FrozenFields)
	}

	changed := validEcuadorIssuerBody()
	changed["ruc"] = "0990012345001"
	changed["establecimiento"] = "002"
	changed["punto_emision"] = "003"
	saved := putEcuadorIssuer(t, sriEnv, sessionID, changed)
	if saved.RUC != "0990012345001" || saved.Establecimiento != "002" || saved.PuntoEmision != "003" {
		t.Fatalf("saved = %+v, want the changed numbering", saved)
	}
}

// TestIssuerRUCFrozenOnceAnInvoiceExists: one invoice, in either environment,
// and the RUC is immutable; the read says so; everything else still saves.
func TestIssuerRUCFrozenOnceAnInvoiceExists(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	issueOK(t, sessionID, validInvoiceBody())

	view := getFrozenIssuer(t, sessionID)
	slices.Sort(view.FrozenFields)
	if want := []string{"establecimiento", "punto_emision", "ruc"}; !slices.Equal(view.FrozenFields, want) {
		t.Fatalf("frozen_fields = %v, want %v", view.FrozenFields, want)
	}

	changed := validEcuadorIssuerBody()
	changed["ruc"] = "0990012345001"
	resp, body := sriEnv.put(t, ecuadorIssuerPath, changed, authHeader(sessionID))
	expectFrozen(t, "PUT ruc", resp, body, "ruc")
	if getFrozenIssuer(t, sessionID).RUC != "1790012345001" {
		t.Fatal("a refused save changed the RUC")
	}

	// The invoice was issued under test; flipping to production does not
	// thaw the RUC — an invoice in EITHER environment freezes it.
	flip := validEcuadorIssuerBody()
	flip["environment"] = "production"
	putEcuadorIssuer(t, sriEnv, sessionID, flip)
	flip["ruc"] = "0990012345001"
	resp, body = sriEnv.put(t, ecuadorIssuerPath, flip, authHeader(sessionID))
	expectFrozen(t, "PUT ruc under production", resp, body, "ruc")

	// Everything else still saves — and the frozen fields, sent unchanged,
	// are no obstacle.
	edited := validEcuadorIssuerBody()
	edited["razon_social"] = "Red Planet Tribe Renamed S.A.S."
	edited["nombre_comercial"] = ""
	edited["direccion_matriz"] = "Calle Nueva 1, Quito"
	edited["direccion_establecimiento"] = "Calle Nueva 2, Quito"
	edited["obligado_contabilidad"] = false
	edited["regimen"] = "rimpe_contribuyente"
	edited["agente_retencion"] = "NAC-DNCRASC20-00000001"
	saved := putEcuadorIssuer(t, sriEnv, sessionID, edited)
	if saved.RazonSocial != "Red Planet Tribe Renamed S.A.S." || saved.DireccionMatriz != "Calle Nueva 1, Quito" ||
		saved.Regimen != "rimpe_contribuyente" || saved.ObligadoContabilidad || saved.AgenteRetencion == nil {
		t.Fatalf("editable fields did not save: %+v", saved)
	}
}

// TestIssuerNumberingFrozenOnceASequenceExists: after the first issue a
// sequence row exists under 001/001, so neither code may change; the
// refusal names the field.
func TestIssuerNumberingFrozenOnceASequenceExists(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	issueOK(t, sessionID, validInvoiceBody())

	estab := validEcuadorIssuerBody()
	estab["establecimiento"] = "002"
	resp, body := sriEnv.put(t, ecuadorIssuerPath, estab, authHeader(sessionID))
	expectFrozen(t, "PUT establecimiento", resp, body, "establecimiento")

	pto := validEcuadorIssuerBody()
	pto["punto_emision"] = "002"
	resp, body = sriEnv.put(t, ecuadorIssuerPath, pto, authHeader(sessionID))
	expectFrozen(t, "PUT punto_emision", resp, body, "punto_emision")

	after := getFrozenIssuer(t, sessionID)
	if after.Establecimiento != "001" || after.PuntoEmision != "001" {
		t.Fatalf("a refused save changed the numbering: %s-%s", after.Establecimiento, after.PuntoEmision)
	}
}
