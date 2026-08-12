package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The Customer Avatar: one optional profile image, uploaded from "My info" or
// seeded from a Google Sign-In into an empty slot, removable back to initials.
//
// The properties worth attacking:
//
//   - Only a full Customer Session writes. A Confirmation Link session may not
//     mint an upload URL, attach, or remove — the same line "My info" draws.
//   - A Customer may attach only a key under their own prefix. Another
//     Customer's key, or an arbitrary object, is refused.
//   - Google seeding is fill-only. It lands in an empty slot, never overwrites
//     an upload, and is never refreshed on a later sign-in.
//   - A picture that cannot be fetched costs the Avatar, never the sign-in.

const (
	customerAvatarUploadURLPath = "/api/v1/customer/profile/avatar-upload-url"
	customerAvatarPath          = "/api/v1/customer/profile/avatar"
)

type avatarUploadTicket struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	PublicURL string `json:"public_url"`
}

func createAvatarUploadURL(t *testing.T, env *testEnv, token string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	return env.post(t, customerAvatarUploadURLPath, map[string]any{
		"content_type": "image/png",
		"file_name":    "me.png",
	}, headers)
}

func createAvatarUploadURLOK(t *testing.T, env *testEnv, token string) avatarUploadTicket {
	t.Helper()
	resp, body := createAvatarUploadURL(t, env, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("avatar upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var ticket avatarUploadTicket
	if err := json.Unmarshal(body.Data, &ticket); err != nil {
		t.Fatalf("decode upload ticket: %v", err)
	}
	return ticket
}

func attachAvatar(t *testing.T, env *testEnv, token, key string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	return env.put(t, customerAvatarPath, map[string]any{"image_key": key}, headers)
}

func removeAvatar(t *testing.T, env *testEnv, token string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	return env.deleteJSON(t, customerAvatarPath, nil, headers)
}

// avatarProfileView decodes the profile the avatar writes return.
type avatarProfileView struct {
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
}

func decodeAvatarProfile(t *testing.T, body envelope) avatarProfileView {
	t.Helper()
	var view avatarProfileView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode profile view: %v", err)
	}
	return view
}

// TestCustomerAvatarUploadAttachAndRemove walks the whole editable life of an
// uploaded Avatar: mint a URL under the Customer's own prefix, attach the key,
// see it on both the profile and the session (the header chip reads the
// session), and remove it back to nothing.
func TestCustomerAvatarUploadAttachAndRemove(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	ticket := createAvatarUploadURLOK(t, env, token)
	if !strings.Contains(ticket.ObjectKey, "avatars/") {
		t.Fatalf("object key = %q, want an avatars/ key", ticket.ObjectKey)
	}
	if ticket.UploadURL == "" || !strings.Contains(ticket.PublicURL, ticket.ObjectKey) {
		t.Fatalf("upload_url=%q public_url=%q object_key=%q", ticket.UploadURL, ticket.PublicURL, ticket.ObjectKey)
	}

	resp, body := attachAvatar(t, env, token, ticket.ObjectKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attach status=%d error=%+v", resp.StatusCode, body.Error)
	}
	attached := decodeAvatarProfile(t, body)
	if attached.AvatarURL == nil || *attached.AvatarURL != ticket.PublicURL {
		t.Fatalf("profile avatar_url = %v, want %q", attached.AvatarURL, ticket.PublicURL)
	}

	// The session payload — what the Storefront header renders from — carries it.
	if session := readCustomerSession(t, env, token); session.AvatarURL == nil || *session.AvatarURL != ticket.PublicURL {
		t.Fatalf("session avatar_url = %v, want %q", session.AvatarURL, ticket.PublicURL)
	}

	resp, body = removeAvatar(t, env, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if removed := decodeAvatarProfile(t, body); removed.AvatarURL != nil {
		t.Fatalf("profile avatar_url after removal = %q, want null", *removed.AvatarURL)
	}
	if session := readCustomerSession(t, env, token); session.AvatarURL != nil {
		t.Fatalf("session avatar_url after removal = %q, want null", *session.AvatarURL)
	}
}

// TestCustomerAvatarRefusesForeignKey: an upload ticket is scoped to the
// Customer it was minted for. Attaching someone else's key — or any key outside
// your own prefix — is refused, and nothing moves.
func TestCustomerAvatarRefusesForeignKey(t *testing.T) {
	env := setupTest(t)
	anaToken := customerSignIn(t, env, "ana@example.com")
	bobToken := customerSignIn(t, env, "bob@example.com")

	anaTicket := createAvatarUploadURLOK(t, env, anaToken)

	for _, tc := range []struct {
		name string
		key  string
	}{
		{"another Customer's key", anaTicket.ObjectKey},
		{"an arbitrary object", "logos/some-org/logo.png"},
		{"a traversal attempt", "avatars/../logos/some-org/logo.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := attachAvatar(t, env, bobToken, tc.key)
			assertAPIError(t, resp, body, http.StatusBadRequest, "AVATAR_IMAGE_INVALID")
		})
	}

	if session := readCustomerSession(t, env, bobToken); session.AvatarURL != nil {
		t.Fatalf("bob's avatar_url = %q, want null after only refused attaches", *session.AvatarURL)
	}
}

// TestConfirmationLinkSessionCannotTouchTheAvatar: possession of a forwarded
// confirmation email must not change how a person is pictured — the same
// narrowing the profile PATCH enforces, on all three avatar writes.
func TestConfirmationLinkSessionCannotTouchTheAvatar(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Avatar Fest", "avatar-fest",
		env.fixedClock.Add(30*24*time.Hour), "avatar-link", "ana@example.com", "Ana", "Lopez")

	_, linkSession := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")

	resp, body := createAvatarUploadURL(t, env, linkSession)
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	resp, body = attachAvatar(t, env, linkSession, "avatars/whatever/x.png")
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	resp, body = removeAvatar(t, env, linkSession)
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")
}

// TestCustomerAvatarUploadURLRejectsBadContentType pins the image allowlist at
// the minting step: a key is never handed out for a type the Avatar cannot be.
func TestCustomerAvatarUploadURLRejectsBadContentType(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	resp, body := env.post(t, customerAvatarUploadURLPath, map[string]any{
		"content_type": "image/gif",
	}, authHeader(token))
	assertAPIError(t, resp, body, http.StatusBadRequest, "AVATAR_IMAGE_INVALID")
}

// pictureServer stands in for Google's avatar host: a URL the API can fetch a
// small PNG from, plus a switch to serve garbage instead.
func pictureServer(t *testing.T, contentType string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("not-really-a-png-but-nobody-decodes-it"))
	}))
	t.Cleanup(server.Close)
	return server
}

// googleClaimsWithPicture is verifiedGoogleClaims plus the `picture` claim the
// `profile` scope adds.
func googleClaimsWithPicture(email, pictureURL string) map[string]any {
	claims := verifiedGoogleClaims(email)
	claims["picture"] = pictureURL
	return claims
}

// TestGoogleSignInSeedsAvatarIntoEmptySlot: the first Google Sign-In of a
// Customer with no Avatar re-hosts the picture Google offered and the session
// comes back already wearing it. The stored URL is this platform's own, never
// Google's — the fetch happened server-side and the object now lives in our
// storage.
func TestGoogleSignInSeedsAvatarIntoEmptySlot(t *testing.T) {
	env := setupTest(t)
	pictures := pictureServer(t, "image/png", http.StatusOK)

	googleStub.returns(googleClaimsWithPicture("ana@example.com", pictures.URL+"/photo.png"))
	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	// The Avatar is seeded when the address is proven, before the consent gate;
	// the session that carries it is minted on the far side of the consent step
	// (#251), so this reads the session that step returns.
	data := completeConsentStep(t, env, decodeCustomerVerify(t, body))

	if data.Session.AvatarURL == nil {
		t.Fatal("session avatar_url = null, want a seeded Avatar")
	}
	if !strings.Contains(*data.Session.AvatarURL, "avatars/") {
		t.Fatalf("session avatar_url = %q, want a re-hosted avatars/ URL", *data.Session.AvatarURL)
	}
	if strings.Contains(*data.Session.AvatarURL, pictures.URL) {
		t.Fatalf("session avatar_url = %q, want our storage rather than Google's host", *data.Session.AvatarURL)
	}
}

// TestGoogleSignInNeverOverwritesAnAvatar is the fill-only rule from both
// directions: an uploaded Avatar survives a Google Sign-In, and a seeded one is
// not refreshed when Google offers a different picture later.
func TestGoogleSignInNeverOverwritesAnAvatar(t *testing.T) {
	env := setupTest(t)
	pictures := pictureServer(t, "image/png", http.StatusOK)

	// Upload first, then sign in with Google: the upload wins.
	uploadToken := customerSignIn(t, env, "ana@example.com")
	ticket := createAvatarUploadURLOK(t, env, uploadToken)
	if resp, body := attachAvatar(t, env, uploadToken, ticket.ObjectKey); resp.StatusCode != http.StatusOK {
		t.Fatalf("attach status=%d error=%+v", resp.StatusCode, body.Error)
	}

	googleStub.returns(googleClaimsWithPicture("ana@example.com", pictures.URL+"/other.png"))
	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	data := completeConsentStep(t, env, decodeCustomerVerify(t, body))
	if data.Session.AvatarURL == nil || *data.Session.AvatarURL != ticket.PublicURL {
		t.Fatalf("avatar after google sign-in = %v, want the untouched upload %q", data.Session.AvatarURL, ticket.PublicURL)
	}

	// Seed a second Customer, then sign them in again: set-once, no sync — the
	// first seeded key stays.
	googleStub.returns(googleClaimsWithPicture("bob@example.com", pictures.URL+"/first.png"))
	resp, body = postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	first := completeConsentStep(t, env, decodeCustomerVerify(t, body))
	if first.Session.AvatarURL == nil {
		t.Fatal("bob's first sign-in seeded nothing")
	}

	googleStub.returns(googleClaimsWithPicture("bob@example.com", pictures.URL+"/second.png"))
	resp, body = postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	second := decodeCustomerVerify(t, body)
	if second.Session.AvatarURL == nil || *second.Session.AvatarURL != *first.Session.AvatarURL {
		t.Fatalf("avatar after a second sign-in = %v, want the first seed %q kept", second.Session.AvatarURL, *first.Session.AvatarURL)
	}
}

// TestGoogleSignInSurvivesAnUnfetchablePicture: the Avatar is decoration and
// the sign-in is the point. A picture host that errors, or serves something
// that is not an image, costs the seed and nothing else.
func TestGoogleSignInSurvivesAnUnfetchablePicture(t *testing.T) {
	env := setupTest(t)

	for _, tc := range []struct {
		name    string
		picture func() string
	}{
		{"host errors", func() string { return pictureServer(t, "image/png", http.StatusNotFound).URL }},
		{"not an image", func() string { return pictureServer(t, "text/html", http.StatusOK).URL }},
		{"unreachable host", func() string { return "http://127.0.0.1:1/nope.png" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email := strings.ReplaceAll(tc.name, " ", "-") + "@example.com"
			googleStub.returns(googleClaimsWithPicture(email, tc.picture()))
			resp, body := postGoogleVerify(t, env)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
			}
			data := completeConsentStep(t, env, decodeCustomerVerify(t, body))
			if data.Session.AvatarURL != nil {
				t.Fatalf("avatar_url = %q, want null when the picture cannot be fetched", *data.Session.AvatarURL)
			}
			if data.SessionID == "" {
				t.Fatal("sign-in yielded no session")
			}
		})
	}
}

// TestPasscodeSignInSeedsNothing: only Google offers a picture; the passcode
// door never touches the Avatar.
func TestPasscodeSignInSeedsNothing(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")
	if session := readCustomerSession(t, env, token); session.AvatarURL != nil {
		t.Fatalf("avatar_url after a passcode sign-in = %q, want null", *session.AvatarURL)
	}
}
