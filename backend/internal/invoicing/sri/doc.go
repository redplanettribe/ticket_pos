// Package sri is the Ecuador adapter's document kit for Tax Invoices: the
// pure, network-free pieces whose correctness is a legal artifact, plus the
// SOAP client that carries them to the SRI. It knows nothing about the
// database or HTTP handlers; the invoicing module wires it.
//
// Issuing a factura (ticket #454) is:
//
//	key, _ := sri.NewAccessKey(sri.AccessKeyInput{...})          // clave de acceso
//	built, _ := sri.BuildFactura(sri.Factura{AccessKey: key, ...}) // XML v1.1.0 + totals
//	cert, _ := sri.OpenCertificate(p12, password)                  // .p12 → key + metadata
//	signed, _ := sri.Sign(built.XML, cert, sri.SignOptions{})      // XAdES-BES, RSA-SHA1
//	client := sri.NewClient(env, sri.WithBaseURL(override))
//	rec, _ := client.ValidateComprobante(ctx, signed)              // RECIBIDA / DEVUELTA
//	auth, _ := client.AuthorizationComprobante(ctx, key)           // AUTORIZADO / NO AUTORIZADO / EN PROCESAMIENTO
//
// Resending a rejected document rebuilds and re-signs from the stored
// snapshot with the same AccessKey and Sequential. Verify checks a signed
// document from its bytes alone and is what harnesses use to prove what
// the SRI received.
//
// A RIDE (ticket #456) is rendered from stored data with ComputeTotals,
// FormatCents, Quantity.String, PaymentMethodLabel, IVACode.RatePercent and
// ParseAccessKey; the authorization number is the clave de acceso.
//
// Every date the SRI sees is computed in Guayaquil from the instant given.
// Amounts are integer cents; quantities are millionths.
package sri
