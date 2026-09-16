package service

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Tax Document Archive (#630, spec #629; CONTEXT.md): every document the
// Ecuador Issuer emitted in a range of Emission Dates that the SRI authorized
// in production and that still stands, as the signed XML itself, in one ZIP
// for the platform's accountant.
//
// WHICH DOCUMENTS is decided here, ONCE, by taxDocumentArchiveIncluded and
// taxDocumentArchiveUnsettled. The archive and its pre-download summary (#631)
// both ask the repository through these two and nothing else, so the number
// an operator is shown before downloading is the number of files they get,
// unless a document settles in between.

// taxDocumentArchiveIncluded is what an archive holds: the Issuer's documents
// of every kind (manual, sale, credit_note) that are `authorized` in
// `production`, emitted in the range, both ends included.
//
// "Authorized and still standing" is exactly status = authorized: annulled
// and abandoned are statuses of their own, and Superseded is a relation
// between documents rather than a status, so a Superseded Sale Invoice
// qualifies on its own, beside the Credit Note that undid it.
func taxDocumentArchiveIncluded(issuerID, from, to string) repository.ArchiveCriteria {
	return repository.ArchiveCriteria{
		IssuerID:    issuerID,
		Environment: invoicing.EnvironmentProduction,
		Statuses:    []invoicing.InvoiceStatus{invoicing.InvoiceStatusAuthorized},
		From:        from,
		To:          to,
	}
}

// taxDocumentArchiveUnsettled is what an archive leaves out and COUNTS: the
// Issuer's production documents emitted in the range that the SRI has not
// settled yet, `pending` or `needs_attention`. The finally refused and given
// up on (not_authorized, rejected, withdrawn, annulled, abandoned) are neither
// included nor counted, and an owed document has no Emission Date and belongs
// to no range at all.
func taxDocumentArchiveUnsettled(issuerID, from, to string) repository.ArchiveCriteria {
	return repository.ArchiveCriteria{
		IssuerID:    issuerID,
		Environment: invoicing.EnvironmentProduction,
		Statuses:    []invoicing.InvoiceStatus{invoicing.InvoiceStatusPending, invoicing.InvoiceStatusNeedsAttention},
		From:        from,
		To:          to,
	}
}

// ContentTypeZIP is what the archive is served as.
const ContentTypeZIP = "application/zip"

// TaxDocumentArchive is one archive, validated and ready to stream. Every
// refusal (no Issuer) has been answered by the time one exists, so the
// handler can commit to a 200 before the first byte is written.
type TaxDocumentArchive struct {
	svc      *Service
	issuer   *repository.EcuadorIssuerRow
	operator string
	from     string
	to       string
	// Filename is `comprobantes-<RUC>-<from>-<to>.zip`.
	Filename string
}

// OpenTaxDocumentArchive prepares the archive of the Ecuador Issuer's
// documents emitted from `from` to `to` ("YYYY-MM-DD", both included, already
// validated by the caller) for the Platform Operator whose email is given.
// ISSUER_NOT_FOUND when no Ecuador Issuer has been recorded.
func (s *Service) OpenTaxDocumentArchive(ctx context.Context, operatorEmail, from, to string) (*TaxDocumentArchive, error) {
	issuer, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		return nil, err
	}
	if issuer == nil {
		return nil, invoicing.ErrIssuerNotFound()
	}
	return &TaxDocumentArchive{
		svc:      s,
		issuer:   issuer,
		operator: operatorEmail,
		from:     from,
		to:       to,
		Filename: "comprobantes-" + issuer.Details.RUC + "-" + from + "-" + to + ".zip",
	}, nil
}

// WriteTo streams the ZIP to w: one `<clave de acceso>.xml` per included
// document at the top level, in Emission Date then clave order, each the
// stored signed XML byte for byte, and then the readme as the LAST entry.
//
// NOTHING IS HELD: each row is read, written into the ZIP and dropped before
// the next is read, and archive/zip writes through to w as it goes. There is
// no document cap and no range cap because there is nothing to exhaust.
//
// The readme's factura and Credit Note counts are the files actually written,
// counted as they are written, never a count run beforehand. Its unsettled
// count is read after the documents, so it is as fresh as the archive.
//
// Once this has begun writing, an error cannot become a refusal: the caller
// has already sent its status. The error is logged here and returned for the
// caller to abort the response with, so the client sees a failed download
// rather than a ZIP that silently ends early. The one structured log line is
// written only when the whole archive was.
func (a *TaxDocumentArchive) WriteTo(ctx context.Context, w io.Writer) error {
	if err := a.write(ctx, w); err != nil {
		a.svc.logger.Error("invoicing: tax document archive failed mid-stream",
			"operator_email", a.operator, "from", a.from, "to", a.to, "error", err)
		return err
	}
	return nil
}

func (a *TaxDocumentArchive) write(ctx context.Context, w io.Writer) error {
	s := a.svc
	generatedAt := s.clock()
	archive := zip.NewWriter(w)

	var written repository.ArchiveCounts
	err := s.repo.StreamArchiveDocuments(ctx, taxDocumentArchiveIncluded(a.issuer.Issuer.ID, a.from, a.to), func(doc repository.ArchiveDocument) error {
		entry, err := archive.CreateHeader(&zip.FileHeader{
			Name:     doc.AccessKey + ".xml",
			Method:   zip.Deflate,
			Modified: generatedAt,
		})
		if err != nil {
			return fmt.Errorf("tax document archive: create entry: %w", err)
		}
		if _, err := entry.Write(doc.SignedXML); err != nil {
			return fmt.Errorf("tax document archive: write entry: %w", err)
		}
		switch doc.Kind {
		case invoicing.DocumentKindCreditNote:
			written.CreditNotes++
		case invoicing.DocumentKindSale:
			written.Sale++
		default:
			written.Manual++
		}
		return nil
	})
	if err != nil {
		return err
	}

	unsettled, err := s.repo.CountArchiveDocuments(ctx, taxDocumentArchiveUnsettled(a.issuer.Issuer.ID, a.from, a.to))
	if err != nil {
		return err
	}

	locale := s.staffLocale(ctx, a.operator)
	readme := taxDocumentArchiveReadme{
		Locale:      locale,
		RUC:         a.issuer.Details.RUC,
		RazonSocial: a.issuer.Details.RazonSocial,
		From:        a.from,
		To:          a.to,
		GeneratedAt: generatedAt,
		Facturas:    written.Facturas(),
		CreditNotes: written.CreditNotes,
		Unsettled:   unsettled.Total(),
	}
	entry, err := archive.CreateHeader(&zip.FileHeader{
		Name:     readme.filename(),
		Method:   zip.Deflate,
		Modified: generatedAt,
	})
	if err != nil {
		return fmt.Errorf("tax document archive: create readme: %w", err)
	}
	if _, err := io.WriteString(entry, readme.text()); err != nil {
		return fmt.Errorf("tax document archive: write readme: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("tax document archive: close: %w", err)
	}

	// Who took which period, and what it held (spec #629 story 28): the
	// archive carries buyers' Tax IDs and names, so every one taken is
	// answerable later. No Recipient data is in the line itself.
	s.logger.Info("invoicing: tax document archive generated",
		"operator_email", a.operator,
		"from", a.from,
		"to", a.to,
		"facturas", written.Facturas(),
		"credit_notes", written.CreditNotes,
		"unsettled", unsettled.Total(),
	)
	return nil
}

// taxDocumentArchiveReadme is the note the accountant reads without opening
// the platform: the period, when it was generated, whose documents they are,
// how many of each kind the ZIP holds and how many emitted in the period were
// not settled yet.
type taxDocumentArchiveReadme struct {
	Locale      platform.Locale
	RUC         string
	RazonSocial string
	From        string
	To          string
	GeneratedAt time.Time
	Facturas    int
	CreditNotes int
	Unsettled   int
}

func (r taxDocumentArchiveReadme) filename() string {
	if r.Locale == platform.LocaleES {
		return "LEEME.txt"
	}
	return "README.txt"
}

// text is the readme's body. Plain text with CRLF line ends, so it reads the
// same in Notepad as anywhere else. The generated-at moment is written in
// Ecuador time with its offset, which is unambiguous in both languages.
func (r taxDocumentArchiveReadme) text() string {
	generated := r.GeneratedAt.In(ecuadorTime()).Format("2006-01-02 15:04:05 -07:00")
	var lines []string
	if r.Locale == platform.LocaleES {
		lines = []string{
			"Archivo de comprobantes",
			"",
			"Emisor: " + r.RazonSocial + " (RUC " + r.RUC + ")",
			"Periodo: fechas de emisión del " + r.From + " al " + r.To + ", ambas incluidas",
			"Generado: " + generated + " (hora de Ecuador)",
			"",
			fmt.Sprintf("Facturas: %d", r.Facturas),
			fmt.Sprintf("Notas de crédito: %d", r.CreditNotes),
			fmt.Sprintf("Comprobantes emitidos en el periodo aún sin resolver por el SRI: %d", r.Unsettled),
			"",
			"Cada comprobante es el XML firmado tal como se envió al SRI, nombrado por su clave de acceso.",
			"Solo se incluyen los comprobantes autorizados por el SRI en el ambiente de producción. No se incluyen",
			"los retirados, anulados, abandonados, no autorizados, devueltos ni los del ambiente de pruebas.",
		}
		if r.Unsettled > 0 {
			lines = append(lines, "",
				"Los comprobantes sin resolver no están en este archivo. Descargue el archivo de nuevo cuando el SRI",
				"los haya autorizado para completar el periodo.")
		}
	} else {
		lines = []string{
			"Tax Document Archive",
			"",
			"Issuer: " + r.RazonSocial + " (RUC " + r.RUC + ")",
			"Period: Emission Dates from " + r.From + " to " + r.To + ", both included",
			"Generated: " + generated + " (Ecuador time)",
			"",
			fmt.Sprintf("Facturas: %d", r.Facturas),
			fmt.Sprintf("Credit Notes: %d", r.CreditNotes),
			fmt.Sprintf("Documents emitted in the period not yet settled by the SRI: %d", r.Unsettled),
			"",
			"Each document is the signed XML exactly as it was sent to the SRI, named by its clave de acceso.",
			"Only documents the SRI authorized in its production environment are included. Withdrawn, annulled,",
			"abandoned, not authorized, rejected and test-environment documents are not.",
		}
		if r.Unsettled > 0 {
			lines = append(lines, "",
				"The unsettled documents are not in this archive. Download the archive again once the SRI has",
				"authorized them to complete the period.")
		}
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// ecuadorTime is the Issuer's wall clock, which does not observe daylight
// saving: America/Guayaquil when the zone database is present, UTC-5 when not.
func ecuadorTime() *time.Location {
	if loc, err := time.LoadLocation(platform.EcuadorTimeZone); err == nil {
		return loc
	}
	return time.FixedZone("ECT", -5*60*60)
}
