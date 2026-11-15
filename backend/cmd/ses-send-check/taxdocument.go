package main

import (
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/ride"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// taxDocumentAttachments builds the two files a Tax Document delivery
// carries, the way the delivery step builds them (invoicing/service,
// deliverDocument): the factura XML under its clave, and its RIDE rendered by
// the real renderer. Both open: the XML is what sri.BuildFactura writes and
// the PDF is what ride.Render writes, for one Sale Invoice of two General
// tickets issued now.
//
// The document is in the SRI's test environment, so the RIDE prints
// "PRUEBAS" and nobody can take it for a real invoice. The XML is unsigned:
// the check proves delivery, and signing needs the Issuer's certificate.
func taxDocumentAttachments(now time.Time) ([]platform.EmailAttachment, error) {
	issuer := sri.Issuer{
		RUC:                  "1790012345001",
		RazonSocial:          "TICKET POS S.A.S.",
		NombreComercial:      "Multiticketing",
		DirMatriz:            "Av. Amazonas N24-03 y Colón, Quito",
		DirEstablecimiento:   "Av. Amazonas N24-03 y Colón, Quito",
		Establishment:        "001",
		EmissionPoint:        "001",
		ObligadoContabilidad: false,
		Regime:               sri.RegimeGeneral,
	}
	recipient := invoicing.Recipient{
		TaxIDType: "cedula",
		TaxID:     "1712345675",
		LegalName: "Ana López",
		Email:     "ana.lopez@example.com",
	}
	line := sri.Line{
		Description:    "General - Noche de Jazz",
		Quantity:       sri.QuantityFromInt(2),
		UnitPriceCents: 2500,
		IVA:            sri.IVACode15,
		IVAInclusive:   true,
	}
	const sequential = "000000042"
	// 19 is the SRI's "tarjeta de crédito", what a Storefront card sale is.
	const paymentMethod sri.PaymentMethod = "19"
	issuedOn := now.In(sri.Guayaquil)

	accessKey, err := sri.NewAccessKey(sri.AccessKeyInput{
		IssuedOn: issuedOn, DocumentType: sri.DocumentTypeFactura, RUC: issuer.RUC,
		Environment: sri.EnvironmentTest, Establishment: issuer.Establishment,
		EmissionPoint: issuer.EmissionPoint, Sequential: sequential, NumericCode: "12345678",
	})
	if err != nil {
		return nil, fmt.Errorf("tax document fixture: access key: %w", err)
	}
	built, err := sri.BuildFactura(sri.Factura{
		Environment: sri.EnvironmentTest,
		Issuer:      issuer,
		AccessKey:   accessKey,
		Sequential:  sequential,
		IssuedOn:    issuedOn,
		Recipient: sri.Recipient{
			IDType: sri.RecipientIDCedula, ID: recipient.TaxID,
			LegalName: recipient.LegalName, Email: recipient.Email,
		},
		Lines:         []sri.Line{line},
		PaymentMethod: paymentMethod,
	})
	if err != nil {
		return nil, fmt.Errorf("tax document fixture: factura: %w", err)
	}
	totals := built.Totals
	snapshot := invoicing.SnapshotOf(invoicing.EcuadorIssuerDetails{
		RUC:                      issuer.RUC,
		RazonSocial:              issuer.RazonSocial,
		NombreComercial:          issuer.NombreComercial,
		DireccionMatriz:          issuer.DirMatriz,
		DireccionEstablecimiento: issuer.DirEstablecimiento,
		Establecimiento:          issuer.Establishment,
		PuntoEmision:             issuer.EmissionPoint,
		ObligadoContabilidad:     issuer.ObligadoContabilidad,
		Regimen:                  invoicing.RegimenGeneral,
	})

	inv := &invoicing.Invoice{
		ID:            "ses-send-check",
		Kind:          invoicing.DocumentKindSale,
		Country:       invoicing.CountryEcuador,
		Environment:   invoicing.EnvironmentTest,
		Status:        invoicing.InvoiceStatusAuthorized,
		IssuedOn:      issuedOn,
		IssuedAt:      now,
		Recipient:     recipient,
		Issuer:        &snapshot,
		Currency:      "USD",
		SubtotalCents: totals.SubtotalCents,
		DiscountCents: totals.DiscountCents,
		IVACents:      totals.IVACents,
		TotalCents:    totals.TotalCents,
		PaymentMethod: string(paymentMethod),
		SignedXML:     built.XML,
		Lines: []invoicing.InvoiceLine{{
			Position:           1,
			Description:        line.Description,
			QuantityMillionths: int64(line.Quantity),
			UnitPriceCents:     line.UnitPriceCents,
			IVARate:            invoicing.IVARate15,
			BaseCents:          totals.Lines[0].BaseCents,
			IVACents:           totals.Lines[0].IVACents,
		}},
		TicketSaleID:        "ses-send-check",
		SaleConfirmationRef: "TS-2026-0042",
		IVARate:             invoicing.SaleInvoiceIVARate,
	}
	ec := &invoicing.EcuadorInvoiceDetails{
		CodDoc:        sri.DocumentTypeFactura,
		Estab:         issuer.Establishment,
		PtoEmi:        issuer.EmissionPoint,
		Secuencial:    42,
		AccessKey:     accessKey,
		Authorization: &invoicing.Authorization{Number: accessKey, Date: now},
	}
	rideDoc, err := ride.Render(inv, ec)
	if err != nil {
		return nil, fmt.Errorf("tax document fixture: ride: %w", err)
	}
	return []platform.EmailAttachment{
		{Filename: accessKey + ".xml", ContentType: invoicing.ContentTypeXML, Body: built.XML},
		{Filename: rideDoc.Filename, ContentType: rideDoc.ContentType, Body: rideDoc.Body},
	}, nil
}
