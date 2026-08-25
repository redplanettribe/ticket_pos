package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Ecuador Issuer (#451, parent #450, ADR 0059): the platform's own
// registration with the SRI, recorded by a Platform Operator from the Operator
// Dashboard. These tests are the seam the spec names — HTTP in, HTTP out — and
// they say nothing about the module's tables or its layering.

const ecuadorIssuerPath = "/api/v1/operator/invoicing/issuers/ec"

// ecuadorIssuerView is the Issuer as the API returns it.
type ecuadorIssuerView struct {
	ID                       string  `json:"id"`
	Country                  string  `json:"country"`
	Environment              string  `json:"environment"`
	RUC                      string  `json:"ruc"`
	RazonSocial              string  `json:"razon_social"`
	NombreComercial          string  `json:"nombre_comercial"`
	DireccionMatriz          string  `json:"direccion_matriz"`
	DireccionEstablecimiento string  `json:"direccion_establecimiento"`
	Establecimiento          string  `json:"establecimiento"`
	PuntoEmision             string  `json:"punto_emision"`
	ObligadoContabilidad     bool    `json:"obligado_contabilidad"`
	Regimen                  string  `json:"regimen"`
	AgenteRetencion          *string `json:"agente_retencion"`
	CreatedAt                string  `json:"created_at"`
	UpdatedAt                string  `json:"updated_at"`
}

// validEcuadorIssuerBody is a complete, valid Issuer as an operator would type
// it. The RUC is a company RUC (third digit 9), accepted on structure alone by
// the platform's Tax ID validation.
func validEcuadorIssuerBody() map[string]any {
	return map[string]any{
		"environment":               "test",
		"ruc":                       "1790012345001",
		"razon_social":              "Red Planet Tribe S.A.S.",
		"nombre_comercial":          "Multiticketing",
		"direccion_matriz":          "Av. Amazonas N21-147, Quito",
		"direccion_establecimiento": "Av. Amazonas N21-147, Quito",
		"establecimiento":           "001",
		"punto_emision":             "001",
		"obligado_contabilidad":     true,
		"regimen":                   "general",
		"agente_retencion":          nil,
	}
}

// sameEcuadorIssuer compares two views field by field, the optional agente de
// retención by value rather than by pointer.
func sameEcuadorIssuer(a, b ecuadorIssuerView) bool {
	if (a.AgenteRetencion == nil) != (b.AgenteRetencion == nil) {
		return false
	}
	if a.AgenteRetencion != nil && *a.AgenteRetencion != *b.AgenteRetencion {
		return false
	}
	a.AgenteRetencion, b.AgenteRetencion = nil, nil
	return a == b
}

func putEcuadorIssuer(t *testing.T, env *testEnv, sessionID string, body map[string]any) ecuadorIssuerView {
	t.Helper()
	resp, envelope := env.put(t, ecuadorIssuerPath, body, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT issuer status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	if envelope.Error != nil {
		t.Fatalf("PUT issuer returned error %+v", envelope.Error)
	}
	var view ecuadorIssuerView
	if err := json.Unmarshal(envelope.Data, &view); err != nil {
		t.Fatalf("decode issuer: %v", err)
	}
	return view
}

// TestEcuadorIssuerIsNoneUntilRecorded: before the first save the Issuer is an
// ordinary absence — 200 with a null payload, as the Payout Profile is — and
// never an error. The page renders an empty form from it.
func TestEcuadorIssuerIsNoneUntilRecorded(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	resp, envelope := env.get(t, ecuadorIssuerPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET issuer status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	if envelope.Error != nil {
		t.Fatalf("GET issuer returned error %+v", envelope.Error)
	}
	if string(envelope.Data) != "null" {
		t.Fatalf("GET issuer before any save data=%s, want null", envelope.Data)
	}
}

// TestEcuadorIssuerIsCreatedOnFirstSaveAndUpdatedAfter: PUT creates the Issuer
// the first time and updates it afterwards; every field round-trips; the id is
// stable across saves because there is only ever one Ecuador Issuer.
func TestEcuadorIssuerIsCreatedOnFirstSaveAndUpdatedAfter(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	created := putEcuadorIssuer(t, env, sessionID, validEcuadorIssuerBody())
	if created.ID == "" || created.Country != "ec" || created.Environment != "test" {
		t.Fatalf("created issuer = %+v; want an id, country ec, environment test", created)
	}
	if created.RUC != "1790012345001" || created.RazonSocial != "Red Planet Tribe S.A.S." ||
		created.NombreComercial != "Multiticketing" ||
		created.DireccionMatriz != "Av. Amazonas N21-147, Quito" ||
		created.DireccionEstablecimiento != "Av. Amazonas N21-147, Quito" ||
		created.Establecimiento != "001" || created.PuntoEmision != "001" ||
		!created.ObligadoContabilidad || created.Regimen != "general" ||
		created.AgenteRetencion != nil {
		t.Fatalf("created issuer did not round-trip: %+v", created)
	}

	// Reloading shows what was saved.
	var loaded ecuadorIssuerView
	operatorGetOK(t, env, sessionID, ecuadorIssuerPath, &loaded)
	if !sameEcuadorIssuer(loaded, created) {
		t.Fatalf("GET after create = %+v, want %+v", loaded, created)
	}

	// The second save updates every editable detail in place.
	body := validEcuadorIssuerBody()
	body["razon_social"] = "Red Planet Tribe Cía. Ltda."
	body["nombre_comercial"] = ""
	body["direccion_establecimiento"] = "Calle Larga 1-23, Cuenca"
	body["establecimiento"] = "002"
	body["punto_emision"] = "010"
	body["obligado_contabilidad"] = false
	body["regimen"] = "rimpe_contribuyente"
	body["agente_retencion"] = "NAC-DNCRASC20-00000001"
	updated := putEcuadorIssuer(t, env, sessionID, body)
	if updated.ID != created.ID {
		t.Fatalf("second save changed the id: %s -> %s", created.ID, updated.ID)
	}
	if updated.RazonSocial != "Red Planet Tribe Cía. Ltda." || updated.NombreComercial != "" ||
		updated.DireccionEstablecimiento != "Calle Larga 1-23, Cuenca" ||
		updated.Establecimiento != "002" || updated.PuntoEmision != "010" ||
		updated.ObligadoContabilidad || updated.Regimen != "rimpe_contribuyente" ||
		updated.AgenteRetencion == nil || *updated.AgenteRetencion != "NAC-DNCRASC20-00000001" {
		t.Fatalf("updated issuer did not round-trip: %+v", updated)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Fatalf("update moved created_at: %s -> %s", created.CreatedAt, updated.CreatedAt)
	}

	var reloaded ecuadorIssuerView
	operatorGetOK(t, env, sessionID, ecuadorIssuerPath, &reloaded)
	if !sameEcuadorIssuer(reloaded, updated) {
		t.Fatalf("GET after update = %+v, want %+v", reloaded, updated)
	}
}

// TestEcuadorIssuerEnvironmentFlipsFreely: the environment is test (SRI
// pruebas) or production (SRI producción) and the operator may switch it in
// either direction at any time — certifying against pruebas first, then going
// live, then back to pruebas to try something.
func TestEcuadorIssuerEnvironmentFlipsFreely(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	body := validEcuadorIssuerBody()
	for _, environment := range []string{"test", "production", "test", "production"} {
		body["environment"] = environment
		saved := putEcuadorIssuer(t, env, sessionID, body)
		if saved.Environment != environment {
			t.Fatalf("saved environment=%q, want %q", saved.Environment, environment)
		}
	}

	body["environment"] = "staging"
	resp, envelope := env.put(t, ecuadorIssuerPath, body, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || envelope.Error == nil || envelope.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("unknown environment status=%d error=%+v, want 400 VALIDATION_FAILED", resp.StatusCode, envelope.Error)
	}
	assertFieldError(t, envelope.Error.Details, "environment")
}

// TestEcuadorIssuerValidation: the RUC is validated as the platform validates
// a `ruc` Tax ID, establecimiento and punto de emisión are three digits,
// régimen is one of three values, and the required texts are required. Every
// failing field is named at once, and nothing is stored when any fails.
func TestEcuadorIssuerValidation(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	cases := []struct {
		name  string
		field string
		value any
	}{
		{"ruc too short", "ruc", "179001234500"},
		{"ruc with letters", "ruc", "17900123450A1"},
		{"ruc with a province that does not exist", "ruc", "9990012345001"},
		{"ruc natural person with a bad check digit", "ruc", "1712345678001"},
		{"ruc blank", "ruc", ""},
		{"establecimiento two digits", "establecimiento", "01"},
		{"establecimiento four digits", "establecimiento", "0001"},
		{"establecimiento letters", "establecimiento", "A01"},
		{"punto_emision two digits", "punto_emision", "01"},
		{"punto_emision blank", "punto_emision", ""},
		{"regimen unknown", "regimen", "rimpe"},
		{"regimen blank", "regimen", ""},
		{"razon_social blank", "razon_social", "   "},
		{"direccion_matriz blank", "direccion_matriz", ""},
		{"direccion_establecimiento blank", "direccion_establecimiento", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validEcuadorIssuerBody()
			body[tc.field] = tc.value
			resp, envelope := env.put(t, ecuadorIssuerPath, body, authHeader(sessionID))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d error=%+v, want 400", resp.StatusCode, envelope.Error)
			}
			if envelope.Error == nil || envelope.Error.Code != "VALIDATION_FAILED" {
				t.Fatalf("error=%+v, want VALIDATION_FAILED", envelope.Error)
			}
			assertFieldError(t, envelope.Error.Details, tc.field)
		})
	}

	// Several wrong at once: all of them are named in one answer.
	body := validEcuadorIssuerBody()
	body["ruc"] = "123"
	body["establecimiento"] = "1"
	body["regimen"] = "other"
	resp, envelope := env.put(t, ecuadorIssuerPath, body, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || envelope.Error == nil {
		t.Fatalf("status=%d error=%+v, want 400", resp.StatusCode, envelope.Error)
	}
	assertFieldError(t, envelope.Error.Details, "ruc")
	assertFieldError(t, envelope.Error.Details, "establecimiento")
	assertFieldError(t, envelope.Error.Details, "regimen")

	// Nothing was stored by any of the refused saves.
	resp, envelope = env.get(t, ecuadorIssuerPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK || string(envelope.Data) != "null" {
		t.Fatalf("GET after refused saves status=%d data=%s, want 200 null", resp.StatusCode, envelope.Data)
	}
}

// TestEcuadorIssuerIsOperatorOnly: every invoicing route refuses a missing
// session with 401 and a signed-in Member who is not on the operator allowlist
// with 403 — an Org Admin included, since org roles grant nothing platform-wide.
// The GET is also on the namespace-wide list in operator_test.go; the PUT is
// asserted here because that list only knows how to send one kind of body.
func TestEcuadorIssuerIsOperatorOnly(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		resp, envelope := env.doJSON(t, method, ecuadorIssuerPath, validEcuadorIssuerBody(), nil)
		if resp.StatusCode != http.StatusUnauthorized || envelope.Error == nil || envelope.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("%s unauthenticated status=%d error=%+v, want 401 UNAUTHORIZED", method, resp.StatusCode, envelope.Error)
		}

		resp, envelope = env.doJSON(t, method, ecuadorIssuerPath, validEcuadorIssuerBody(), authHeader(adminSessionID))
		if resp.StatusCode != http.StatusForbidden || envelope.Error == nil || envelope.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s as org_admin status=%d error=%+v, want 403 FORBIDDEN", method, resp.StatusCode, envelope.Error)
		}
	}

	// The refused PUT stored nothing.
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	resp, envelope := env.get(t, ecuadorIssuerPath, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK || string(envelope.Data) != "null" {
		t.Fatalf("GET after refused PUT status=%d data=%s, want 200 null", resp.StatusCode, envelope.Data)
	}
}
