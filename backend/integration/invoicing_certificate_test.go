package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// The Ecuador Issuer's signing certificate (#453, parent #450, ADR 0059): a
// Platform Operator uploads the platform's .p12 with its password, the API
// opens it before storing anything, and keeps the bytes and the password
// encrypted under INVOICING_CERTIFICATE_KEY. Only the metadata is ever read
// back. Every .p12 here is generated in-test from a throwaway key: no fixture
// secret exists anywhere in the repository.

const ecuadorIssuerCertificatePath = ecuadorIssuerPath + "/certificate"

// ecuadorIssuerCertificateView is the certificate as the Issuer read shows it.
type ecuadorIssuerCertificateView struct {
	Subject           string `json:"subject"`
	RUC               string `json:"ruc"`
	NotBefore         string `json:"not_before"`
	NotAfter          string `json:"not_after"`
	FingerprintSHA256 string `json:"fingerprint_sha256"`
	UploadedAt        string `json:"uploaded_at"`
}

// ecuadorIssuerWithCertificateView is the Issuer read with what #453 added.
type ecuadorIssuerWithCertificateView struct {
	ecuadorIssuerView
	Certificate            *ecuadorIssuerCertificateView `json:"certificate"`
	CertificateRUCMismatch bool                          `json:"certificate_ruc_mismatch"`
}

// throwawayP12 builds a self-signed certificate over a fresh key of the given
// kind and packs it as a PKCS#12 file under the password. `serial` is the
// subject's serialNumber attribute, where Ecuadorian CAs put the RUC; "" leaves
// it out so the certificate has no discoverable RUC.
func throwawayP12(t *testing.T, key any, serial, password string) []byte {
	t.Helper()
	subject := pkix.Name{
		CommonName:   "JUAN PEREZ",
		Organization: []string{"TICKET POS S.A.S."},
		Country:      []string{"EC"},
	}
	if serial != "" {
		subject.SerialNumber = serial
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      subject,
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	var public any
	switch k := key.(type) {
	case *rsa.PrivateKey:
		public = &k.PublicKey
	case *ecdsa.PrivateKey:
		public = &k.PublicKey
	default:
		t.Fatalf("unsupported key type %T", key)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, public, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	p12, err := pkcs12.Modern.Encode(key, cert, nil, password)
	if err != nil {
		t.Fatalf("encode p12: %v", err)
	}
	return p12
}

func rsaP12(t *testing.T, serial, password string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return throwawayP12(t, key, serial, password)
}

func ecdsaP12(t *testing.T, serial, password string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	return throwawayP12(t, key, serial, password)
}

// postCertificate uploads a .p12 and its password as multipart/form-data. A
// nil file omits the `file` part; a nil password omits the `password` field.
func postCertificate(t *testing.T, env *testEnv, file []byte, password *string, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if file != nil {
		part, err := writer.CreateFormFile("file", "firma.p12")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(file); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if password != nil {
		if err := writer.WriteField("password", *password); err != nil {
			t.Fatalf("write password: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, env.server.URL+ecuadorIssuerCertificatePath, &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	var envBody envelope
	decodeEnvelope(t, resp, &envBody)
	return resp, envBody
}

func uploadCertificate(t *testing.T, env *testEnv, sessionID string, file []byte, password string) ecuadorIssuerWithCertificateView {
	t.Helper()
	resp, envelope := postCertificate(t, env, file, &password, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST certificate status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	if envelope.Error != nil {
		t.Fatalf("POST certificate returned error %+v", envelope.Error)
	}
	var view ecuadorIssuerWithCertificateView
	if err := json.Unmarshal(envelope.Data, &view); err != nil {
		t.Fatalf("decode issuer: %v", err)
	}
	return view
}

func getIssuerWithCertificate(t *testing.T, env *testEnv, sessionID string) ecuadorIssuerWithCertificateView {
	t.Helper()
	var view ecuadorIssuerWithCertificateView
	operatorGetOK(t, env, sessionID, ecuadorIssuerPath, &view)
	return view
}

// TestEcuadorIssuerCertificateIsUploadedAndKeptEncrypted: the upload answers
// with the certificate's metadata and nothing else — no bytes, no password —
// the Issuer read shows the same, and what is on the row is ciphertext.
func TestEcuadorIssuerCertificateIsUploadedAndKeptEncrypted(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuer := putEcuadorIssuer(t, env, sessionID, validEcuadorIssuerBody())

	p12 := rsaP12(t, "1790012345001", "s3cret")
	uploaded := uploadCertificate(t, env, sessionID, p12, "s3cret")
	if uploaded.ID != issuer.ID {
		t.Fatalf("upload answered with issuer %s, want %s", uploaded.ID, issuer.ID)
	}
	cert := uploaded.Certificate
	if cert == nil {
		t.Fatalf("upload answered with no certificate: %+v", uploaded)
	}
	if cert.RUC != "1790012345001" {
		t.Fatalf("certificate ruc=%q, want the subject's serialNumber", cert.RUC)
	}
	if cert.Subject == "" || cert.Subject == "<nil>" {
		t.Fatalf("certificate subject=%q", cert.Subject)
	}
	if len(cert.FingerprintSHA256) != 64 {
		t.Fatalf("certificate fingerprint=%q, want 64 hex chars", cert.FingerprintSHA256)
	}
	if cert.NotBefore != "2026-01-01T00:00:00Z" || cert.NotAfter != "2028-01-01T00:00:00Z" {
		t.Fatalf("validity window = %s .. %s", cert.NotBefore, cert.NotAfter)
	}
	if cert.UploadedAt == "" {
		t.Fatal("certificate uploaded_at is empty")
	}
	if uploaded.CertificateRUCMismatch {
		t.Fatal("certificate ruc equals the issuer ruc, yet mismatch is reported")
	}

	// The response never carries the bytes or the password, under any name.
	var raw map[string]json.RawMessage
	resp, envelope := env.get(t, ecuadorIssuerPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET issuer status=%d", resp.StatusCode)
	}
	if err := json.Unmarshal(envelope.Data, &raw); err != nil {
		t.Fatalf("decode issuer: %v", err)
	}
	var certRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["certificate"], &certRaw); err != nil {
		t.Fatalf("decode certificate: %v", err)
	}
	for _, key := range []string{"p12", "bytes", "file", "password", "key", "private_key"} {
		if _, ok := certRaw[key]; ok {
			t.Fatalf("certificate payload carries %q", key)
		}
	}
	if bytes.Contains(envelope.Data, []byte("s3cret")) {
		t.Fatal("issuer payload contains the certificate password")
	}

	loaded := getIssuerWithCertificate(t, env, sessionID)
	if loaded.Certificate == nil || *loaded.Certificate != *cert {
		t.Fatalf("GET certificate = %+v, want %+v", loaded.Certificate, cert)
	}

	// SQL because no API exposes the stored bytes, and none ever will: the
	// point of the assertion is that the row holds ciphertext. The .p12 and the
	// password must each be absent from their column as plaintext.
	var storedP12, storedPassword []byte
	if err := env.db.QueryRow(`
		SELECT certificate_p12, certificate_password FROM invoicing_issuers WHERE id = $1
	`, issuer.ID).Scan(&storedP12, &storedPassword); err != nil {
		t.Fatalf("read custody columns: %v", err)
	}
	if len(storedP12) == 0 || len(storedPassword) == 0 {
		t.Fatal("custody columns are empty after upload")
	}
	if bytes.Equal(storedP12, p12) || bytes.Contains(storedP12, p12) {
		t.Fatal("certificate_p12 holds the .p12 in clear")
	}
	if bytes.Contains(storedPassword, []byte("s3cret")) {
		t.Fatal("certificate_password holds the password in clear")
	}
	if bytes.Contains(storedP12, []byte("s3cret")) {
		t.Fatal("certificate_p12 holds the password in clear")
	}
}

// TestEcuadorIssuerCertificateRefusesBadUploads: a wrong password, a file
// without an RSA key, a file that is not a .p12, and a missing part are each
// refused with their own code, and the certificate already on file is exactly
// as it was.
func TestEcuadorIssuerCertificateRefusesBadUploads(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	putEcuadorIssuer(t, env, sessionID, validEcuadorIssuerBody())
	before := uploadCertificate(t, env, sessionID, rsaP12(t, "1790012345001", "s3cret"), "s3cret")

	password := func(s string) *string { return &s }
	cases := []struct {
		name     string
		file     []byte
		password *string
		status   int
		code     string
		field    string
	}{
		{"wrong password", rsaP12(t, "1790012345001", "right"), password("wrong"), http.StatusBadRequest, "CERTIFICATE_PASSWORD_INCORRECT", ""},
		{"no rsa key", ecdsaP12(t, "1790012345001", "s3cret"), password("s3cret"), http.StatusBadRequest, "CERTIFICATE_NO_RSA_KEY", ""},
		{"not a p12", []byte("this is not a pkcs12 file"), password("s3cret"), http.StatusBadRequest, "CERTIFICATE_FILE_INVALID", ""},
		{"no file", nil, password("s3cret"), http.StatusBadRequest, "VALIDATION_FAILED", "file"},
		{"no password", rsaP12(t, "1790012345001", "s3cret"), nil, http.StatusBadRequest, "VALIDATION_FAILED", "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, envelope := postCertificate(t, env, tc.file, tc.password, authHeader(sessionID))
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d error=%+v, want %d", resp.StatusCode, envelope.Error, tc.status)
			}
			if envelope.Error == nil || envelope.Error.Code != tc.code {
				t.Fatalf("error=%+v, want %s", envelope.Error, tc.code)
			}
			if string(envelope.Data) != "null" {
				t.Fatalf("data=%s, want null", envelope.Data)
			}
			if tc.field != "" {
				assertFieldError(t, envelope.Error.Details, tc.field)
			}
		})
	}

	after := getIssuerWithCertificate(t, env, sessionID)
	if after.Certificate == nil || *after.Certificate != *before.Certificate {
		t.Fatalf("refused uploads changed the certificate: %+v -> %+v", before.Certificate, after.Certificate)
	}
}

// TestEcuadorIssuerCertificateReuploadReplaces: a second upload replaces the
// first outright; the fingerprint shown is the new certificate's.
func TestEcuadorIssuerCertificateReuploadReplaces(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	putEcuadorIssuer(t, env, sessionID, validEcuadorIssuerBody())

	first := uploadCertificate(t, env, sessionID, rsaP12(t, "1790012345001", "one"), "one")
	second := uploadCertificate(t, env, sessionID, rsaP12(t, "1790012345001", "two"), "two")
	if first.Certificate.FingerprintSHA256 == second.Certificate.FingerprintSHA256 {
		t.Fatal("re-upload kept the first fingerprint")
	}
	loaded := getIssuerWithCertificate(t, env, sessionID)
	if loaded.Certificate == nil || loaded.Certificate.FingerprintSHA256 != second.Certificate.FingerprintSHA256 {
		t.Fatalf("GET after re-upload shows %+v, want the second certificate", loaded.Certificate)
	}
}

// TestEcuadorIssuerCertificateRUCMismatchIsAWarning: the read says whether the
// certificate's RUC differs from the Issuer's — a warning the page shows, never
// a refusal — and says nothing when the certificate has no discoverable RUC.
// Saving the Issuer under the certificate's RUC clears it.
func TestEcuadorIssuerCertificateRUCMismatchIsAWarning(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	putEcuadorIssuer(t, env, sessionID, validEcuadorIssuerBody()) // RUC 1790012345001

	mismatched := uploadCertificate(t, env, sessionID, rsaP12(t, "0990012345001", "s3cret"), "s3cret")
	if mismatched.Certificate.RUC != "0990012345001" || !mismatched.CertificateRUCMismatch {
		t.Fatalf("certificate under another RUC: ruc=%q mismatch=%v, want 0990012345001 true", mismatched.Certificate.RUC, mismatched.CertificateRUCMismatch)
	}

	body := validEcuadorIssuerBody()
	body["ruc"] = "0990012345001"
	putEcuadorIssuer(t, env, sessionID, body)
	if loaded := getIssuerWithCertificate(t, env, sessionID); loaded.CertificateRUCMismatch {
		t.Fatal("issuer saved under the certificate's RUC still reports a mismatch")
	}

	// The PUT does not touch the certificate.
	if loaded := getIssuerWithCertificate(t, env, sessionID); loaded.Certificate == nil ||
		loaded.Certificate.FingerprintSHA256 != mismatched.Certificate.FingerprintSHA256 {
		t.Fatalf("saving the issuer changed the certificate: %+v", loaded.Certificate)
	}

	noRUC := uploadCertificate(t, env, sessionID, rsaP12(t, "", "s3cret"), "s3cret")
	if noRUC.Certificate.RUC != "" || noRUC.CertificateRUCMismatch {
		t.Fatalf("certificate without a RUC: ruc=%q mismatch=%v, want \"\" false", noRUC.Certificate.RUC, noRUC.CertificateRUCMismatch)
	}
}

// TestEcuadorIssuerCertificateNeedsAnIssuer: the certificate lives on the
// Issuer row, so there is nowhere to put it until the Issuer is recorded.
func TestEcuadorIssuerCertificateNeedsAnIssuer(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	password := "s3cret"
	resp, envelope := postCertificate(t, env, rsaP12(t, "1790012345001", password), &password, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound || envelope.Error == nil || envelope.Error.Code != "ISSUER_NOT_FOUND" {
		t.Fatalf("upload before an issuer status=%d error=%+v, want 404 ISSUER_NOT_FOUND", resp.StatusCode, envelope.Error)
	}
}

// TestEcuadorIssuerCertificateWithoutKeyConfigured: with no
// INVOICING_CERTIFICATE_KEY the app boots and serves the Issuer as before, and
// only the upload fails — with the one code the page shows as "the server has
// no certificate key", never as "bad file". A second app over the same database
// stands in for that deployment.
func TestEcuadorIssuerCertificateWithoutKeyConfigured(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	putEcuadorIssuer(t, env, sessionID, validEcuadorIssuerBody())

	keyless := startAppWithoutCertificateKey(t)

	// Everything else works: the Issuer reads and saves through the keyless app.
	loaded := getIssuerWithCertificate(t, keyless, sessionID)
	if loaded.RUC != "1790012345001" || loaded.Certificate != nil {
		t.Fatalf("keyless GET issuer = %+v", loaded)
	}
	putEcuadorIssuer(t, keyless, sessionID, validEcuadorIssuerBody())

	password := "s3cret"
	resp, envelope := postCertificate(t, keyless, rsaP12(t, "1790012345001", password), &password, authHeader(sessionID))
	if resp.StatusCode != http.StatusServiceUnavailable || envelope.Error == nil || envelope.Error.Code != "CERTIFICATE_KEY_NOT_CONFIGURED" {
		t.Fatalf("keyless upload status=%d error=%+v, want 503 CERTIFICATE_KEY_NOT_CONFIGURED", resp.StatusCode, envelope.Error)
	}
	if after := getIssuerWithCertificate(t, env, sessionID); after.Certificate != nil {
		t.Fatalf("keyless upload stored a certificate: %+v", after.Certificate)
	}
}

// TestEcuadorIssuerCertificateIsOperatorOnly: the upload is refused without a
// session and refused to a signed-in Member off the operator allowlist, and
// neither attempt stores anything.
func TestEcuadorIssuerCertificateIsOperatorOnly(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	putEcuadorIssuer(t, env, operatorSessionID, validEcuadorIssuerBody())
	adminSessionID := orgAdminSession(t, env)

	password := "s3cret"
	resp, envelope := postCertificate(t, env, rsaP12(t, "1790012345001", password), &password, nil)
	if resp.StatusCode != http.StatusUnauthorized || envelope.Error == nil || envelope.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("unauthenticated upload status=%d error=%+v, want 401 UNAUTHORIZED", resp.StatusCode, envelope.Error)
	}
	resp, envelope = postCertificate(t, env, rsaP12(t, "1790012345001", password), &password, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusForbidden || envelope.Error == nil || envelope.Error.Code != "FORBIDDEN" {
		t.Fatalf("org_admin upload status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, envelope.Error)
	}
	if after := getIssuerWithCertificate(t, env, operatorSessionID); after.Certificate != nil {
		t.Fatalf("refused uploads stored a certificate: %+v", after.Certificate)
	}
}

// startAppWithoutCertificateKey boots a second app over the shared database
// with no INVOICING_CERTIFICATE_KEY, the way a deployment that has not
// provisioned the secret runs, and tears it down with the test.
func startAppWithoutCertificateKey(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	cfg := platform.Config{
		DatabaseURL:       sharedConnStr,
		RunMigrations:     false,
		StorefrontBaseURL: "http://storefront.example",
		Fees: platform.FeeConfig{
			FeeBasisPoints:    platform.DefaultPlatformFeeBasisPoints,
			FeeIVABasisPoints: platform.DefaultPlatformFeeIVABasisPoints,
		},
	}
	app, err := server.NewApp(ctx, cfg,
		server.WithEmailSender(sharedEmail),
		server.WithClock(func() time.Time { return fixedClock }),
		server.WithObjectStorage(&mockObjectStorage{}),
	)
	if err != nil {
		t.Fatalf("new keyless app: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterRoutes(mux, app)
	srv := httptest.NewServer(platform.RequestIDMiddleware(mux))
	t.Cleanup(func() {
		srv.Close()
		_ = app.Close()
	})
	return &testEnv{
		server:     srv,
		db:         app.DB.Pool,
		email:      sharedEmail,
		fixedClock: fixedClock,
		service:    app.IdentityService,
	}
}
