package invoicing

import (
	"bytes"
	"errors"
	"testing"
)

func testKey(b byte) []byte { return bytes.Repeat([]byte{b}, CustodyKeyLength) }

func TestCustodyRoundTripsAndNeverStoresPlaintext(t *testing.T) {
	custody, err := NewCustody(testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	if !custody.Configured() {
		t.Fatal("custody with a key reports unconfigured")
	}
	secret := []byte("the .p12 bytes and the password: s3cret")
	sealed, err := custody.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, secret) || bytes.Contains(sealed, []byte("s3cret")) {
		t.Fatal("sealed value contains the plaintext")
	}
	opened, err := custody.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, secret) {
		t.Fatalf("opened = %q, want %q", opened, secret)
	}

	again, err := custody.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(again, sealed) {
		t.Fatal("two seals of one plaintext produced the same bytes: the nonce is not fresh")
	}
}

func TestCustodyRefusesTamperingAndAnotherKey(t *testing.T) {
	custody, _ := NewCustody(testKey(1))
	sealed, err := custody.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := custody.Open(tampered); !errors.Is(err, ErrCustodyOpen) {
		t.Fatalf("open tampered: err = %v, want ErrCustodyOpen", err)
	}
	if _, err := custody.Open([]byte("short")); !errors.Is(err, ErrCustodyOpen) {
		t.Fatalf("open short: err = %v, want ErrCustodyOpen", err)
	}

	other, _ := NewCustody(testKey(2))
	if _, err := other.Open(sealed); !errors.Is(err, ErrCustodyOpen) {
		t.Fatalf("open under another key: err = %v, want ErrCustodyOpen", err)
	}
}

func TestCustodyWithoutAKeyIsUnconfigured(t *testing.T) {
	custody, err := NewCustody(nil)
	if err != nil {
		t.Fatal(err)
	}
	if custody.Configured() {
		t.Fatal("custody without a key reports configured")
	}
	if _, err := custody.Seal([]byte("x")); !errors.Is(err, ErrCustodyNotConfigured) {
		t.Fatalf("seal: err = %v, want ErrCustodyNotConfigured", err)
	}
	if _, err := custody.Open([]byte("x")); !errors.Is(err, ErrCustodyNotConfigured) {
		t.Fatalf("open: err = %v, want ErrCustodyNotConfigured", err)
	}
	var nilCustody *Custody
	if nilCustody.Configured() {
		t.Fatal("nil custody reports configured")
	}
}

func TestCustodyRefusesAKeyOfAnotherLength(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		if _, err := NewCustody(bytes.Repeat([]byte{1}, n)); !errors.Is(err, ErrCustodyKeyLength) {
			t.Fatalf("%d-byte key: err = %v, want ErrCustodyKeyLength", n, err)
		}
	}
}
