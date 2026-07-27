package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/repository"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

// The Customer Avatar: one optional profile image per Customer, shown wherever
// the signed-in Customer is represented in the Storefront. Two ways in — the
// Customer uploads one from "My info", or a Google Sign-In seeds one into an
// empty slot (seedAvatarFromGoogle) — and once stored the two are
// indistinguishable: one column, one key scheme, one serving path.
//
// Every write here requires a full Customer Session, the same line UpdateProfile
// draws: a Confirmation Link session proves possession of a forwarded email, and
// that must not earn the right to change how a person is pictured.

// avatarFetchTimeout bounds the fetch of a Google picture during sign-in. The
// person is waiting on the sign-in response, and an Avatar is decoration: better
// none than a hang.
const avatarFetchTimeout = 10 * time.Second

// maxAvatarSeedBytes caps how much of a seeded picture is read. Google avatars
// are tens of kilobytes; anything approaching this limit is not one.
const maxAvatarSeedBytes = 5 << 20

// CreateAvatarUploadURL returns a presigned PUT URL for a Customer Avatar,
// keyed under the signed-in Customer's own object prefix.
func (s *Service) CreateAvatarUploadURL(ctx context.Context, token, contentType, fileName string) (*storage.CoverUploadResult, error) {
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if session.TicketSaleID.Valid {
		return nil, customers.ErrCustomerSessionScopeInsufficient()
	}
	if s.storage == nil {
		return nil, customers.ErrAvatarUploadUnavailable()
	}

	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if !storage.CoverContentTypeAllowed(contentType) {
		return nil, customers.ErrInvalidAvatarImageKey()
	}

	key, err := storage.BuildAvatarObjectKey(customer.ID, contentType, fileName)
	if err != nil {
		return nil, err
	}

	uploadURL, err := s.storage.PresignPut(ctx, key, contentType, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	return &storage.CoverUploadResult{
		UploadURL: uploadURL,
		ObjectKey: key,
		PublicURL: s.storage.PublicURL(key),
	}, nil
}

// UpdateAvatar attaches an uploaded Avatar (imageKey set) or removes the current
// one (imageKey nil), returning the profile as it now stands.
//
// A set key must sit under the signed-in Customer's own prefix — the one
// CreateAvatarUploadURL mints — so no request can attach another Customer's
// image or an arbitrary object. Removal needs no key at all: it is the Customer
// reverting to initials, always allowed.
func (s *Service) UpdateAvatar(ctx context.Context, token string, imageKey *string) (*CustomerProfileView, error) {
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if session.TicketSaleID.Valid {
		return nil, customers.ErrCustomerSessionScopeInsufficient()
	}

	if imageKey != nil {
		key := strings.TrimSpace(*imageKey)
		if !storage.AvatarKeyBelongsToCustomer(key, customer.ID) {
			return nil, customers.ErrInvalidAvatarImageKey()
		}
		imageKey = &key
	}

	updated, err := s.repo.UpdateAvatarKey(ctx, customer.ID, imageKey)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, customers.ErrCustomerSessionNotFound()
	}

	return s.profileView(updated), nil
}

// avatarURL turns a stored Avatar key into the browser-loadable URL the views
// carry, or nil when the Customer has none — or when storage is unconfigured,
// where a key without a host to serve it is worth the same as no Avatar.
func (s *Service) avatarURL(customer *repository.Customer) *string {
	if !customer.AvatarImageKey.Valid || s.storage == nil {
		return nil
	}
	url := s.storage.PublicURL(customer.AvatarImageKey.String)
	return &url
}

// seedAvatarFromGoogle re-hosts a Google Sign-In picture as the Customer's
// Avatar, if and only if they have none. Set-once, no sync: a later change to
// the person's Google photo is never chased, and an Avatar they uploaded is
// never touched — the fill-only rule is enforced atomically by SeedAvatarKey's
// WHERE clause, not by the read that got us here.
//
// Every failure is logged and swallowed. The person is completing a sign-in;
// a picture that cannot be fetched costs them an initials fallback, not the
// session. Returns the customer with the seeded key applied, so the session
// view minted in the same request already shows the Avatar.
func (s *Service) seedAvatarFromGoogle(ctx context.Context, customer *repository.Customer, pictureURL string) *repository.Customer {
	if customer == nil || customer.AvatarImageKey.Valid || s.storage == nil {
		return customer
	}
	pictureURL = strings.TrimSpace(pictureURL)
	if pictureURL == "" {
		return customer
	}

	key, err := s.fetchAndStoreAvatar(ctx, customer.ID, pictureURL)
	if err != nil {
		s.logger.Warn("google avatar seed skipped", "reason", err.Error())
		return customer
	}

	seeded, err := s.repo.SeedAvatarKey(ctx, customer.ID, key)
	if err != nil {
		s.logger.Warn("google avatar seed skipped", "reason", err.Error())
		return customer
	}
	if !seeded {
		// Someone set an Avatar between the read and this write; theirs wins and
		// the fetched object is left orphaned, exactly like a replaced upload.
		return customer
	}

	updated := *customer
	updated.AvatarImageKey.Valid = true
	updated.AvatarImageKey.String = key
	return &updated
}

// fetchAndStoreAvatar downloads the picture Google vouched for and writes it to
// object storage under the Customer's Avatar prefix, returning the new key.
func (s *Service) fetchAndStoreAvatar(ctx context.Context, customerID, pictureURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pictureURL, nil)
	if err != nil {
		return "", fmt.Errorf("build picture request: %w", err)
	}

	resp, err := s.avatarHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch picture: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("picture fetch returned status %d", resp.StatusCode)
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	if !storage.CoverContentTypeAllowed(contentType) {
		return "", fmt.Errorf("picture has unsupported content type %q", contentType)
	}

	// Read fully before writing so a picture over the cap is refused rather than
	// truncated into a corrupt image.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarSeedBytes+1))
	if err != nil {
		return "", fmt.Errorf("read picture: %w", err)
	}
	if len(body) > maxAvatarSeedBytes {
		return "", fmt.Errorf("picture exceeds %d bytes", maxAvatarSeedBytes)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("picture body is empty")
	}

	key, err := storage.BuildAvatarObjectKey(customerID, contentType, "")
	if err != nil {
		return "", err
	}
	if err := s.storage.Put(ctx, key, contentType, strings.NewReader(string(body))); err != nil {
		return "", fmt.Errorf("store picture: %w", err)
	}
	return key, nil
}
