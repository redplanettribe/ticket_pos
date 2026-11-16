package sri

import (
	"os"
	"testing"
)

// TestReceptorIDIsReadFromTheSignedBytes: the golden factura's buyer ID, and
// one carrying spaces the portal wants typed exactly, are returned verbatim.
func TestReceptorIDIsReadFromTheSignedBytes(t *testing.T) {
	signed, err := os.ReadFile("testdata/golden/factura_one_line_signed.xml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReceptorID(signed)
	if err != nil || got != "0992345678001" {
		t.Fatalf("ReceptorID = %q, %v; want 0992345678001", got, err)
	}
	spaced := []byte(`<?xml version="1.0" encoding="UTF-8"?><factura id="comprobante"><infoFactura><identificacionComprador>AB 12 345 </identificacionComprador></infoFactura></factura>`)
	if got, err := ReceptorID(spaced); err != nil || got != "AB 12 345 " {
		t.Fatalf("ReceptorID = %q, %v; want the spaces kept", got, err)
	}
	if _, err := ReceptorID([]byte(`<factura/>`)); err == nil {
		t.Fatal("ReceptorID on a document without a buyer: want an error")
	}
}
