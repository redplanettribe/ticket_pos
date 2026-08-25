package invoicing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// Certificate custody (#453, ADR 0059): the Issuer's signing .p12 and its
// password are each kept in Postgres AES-256-GCM encrypted under one key
// delivered from Secret Manager as INVOICING_CERTIFICATE_KEY. A leaked backup
// exposes ciphertext only; the key material is decrypted into memory at
// signing time and nowhere else.

// CustodyKeyLength is the key size AES-256-GCM takes.
const CustodyKeyLength = 32

// ErrCustodyKeyLength means NewCustody was handed a key that is not 32 bytes.
var ErrCustodyKeyLength = errors.New("invoicing: custody key must be 32 bytes")

// ErrCustodyNotConfigured means the platform has no certificate key: nothing
// can be sealed or opened. The app runs in this state deliberately — a
// deployment without the secret serves everything but certificate upload and
// signing — which is why it is a runtime error and not a startup failure.
var ErrCustodyNotConfigured = errors.New("invoicing: certificate key is not configured")

// ErrCustodyOpen means a sealed value would not open under this key: the key
// has changed since it was sealed, or the bytes were tampered with. GCM cannot
// tell the two apart and neither can the operator; the remedy for both is a
// re-upload.
var ErrCustodyOpen = errors.New("invoicing: sealed value does not open under the certificate key")

// Custody seals and opens secrets under the certificate key. A Custody built
// without a key (NewCustody(nil)) is a legitimate value: Configured reports
// false and every Seal and Open answers ErrCustodyNotConfigured.
type Custody struct {
	aead cipher.AEAD
}

// NewCustody builds a Custody over a 32-byte key, or an unconfigured one over
// a nil key. Any other length is refused: a truncated key must never quietly
// become a different key.
func NewCustody(key []byte) (*Custody, error) {
	if key == nil {
		return &Custody{}, nil
	}
	if len(key) != CustodyKeyLength {
		return nil, fmt.Errorf("%w, got %d", ErrCustodyKeyLength, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("invoicing: custody cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("invoicing: custody gcm: %w", err)
	}
	return &Custody{aead: aead}, nil
}

// Configured reports whether a key is present.
func (c *Custody) Configured() bool {
	return c != nil && c.aead != nil
}

// Seal encrypts plaintext under the key with a fresh random nonce and returns
// nonce || ciphertext || tag, the only shape Open accepts. Two seals of the
// same plaintext never produce the same bytes.
func (c *Custody) Seal(plaintext []byte) ([]byte, error) {
	if !c.Configured() {
		return nil, ErrCustodyNotConfigured
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("invoicing: custody nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts a value Seal produced. The plaintext is the caller's to keep
// in memory for as long as it is needed and no longer.
func (c *Custody) Open(sealed []byte) ([]byte, error) {
	if !c.Configured() {
		return nil, ErrCustodyNotConfigured
	}
	nonceSize := c.aead.NonceSize()
	if len(sealed) < nonceSize+c.aead.Overhead() {
		return nil, ErrCustodyOpen
	}
	plaintext, err := c.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return nil, ErrCustodyOpen
	}
	return plaintext, nil
}
