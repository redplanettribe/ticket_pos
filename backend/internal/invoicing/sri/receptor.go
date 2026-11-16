package sri

import (
	"errors"

	"github.com/beevik/etree"
)

// ReceptorID is the buyer's identification EXACTLY as the signed comprobante
// carries it - infoFactura/identificacionComprador, or infoNotaCredito's on
// a Credit Note - spaces and all (#745, spec #742). The SRI portal's
// annulment form asks for the receptor's ID "exactamente como consta en XML
// del comprobante, incluido espacios", so the platform reads it from the
// bytes the SRI holds rather than from the Recipient it believes it sent.
func ReceptorID(signed []byte) (string, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(signed); err != nil {
		return "", err
	}
	for _, path := range []string{"./factura/infoFactura/identificacionComprador", "./notaCredito/infoNotaCredito/identificacionComprador"} {
		if el := doc.FindElement(path); el != nil {
			return el.Text(), nil
		}
	}
	return "", errors.New("sri: no identificacionComprador in the comprobante")
}
