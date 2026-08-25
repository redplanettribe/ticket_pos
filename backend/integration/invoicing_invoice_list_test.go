package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// invoiceListView is the invoices list as the API returns it: the ADR-0006
// nested { data, pagination } envelope.
type invoiceListView struct {
	Data []struct {
		ID          string `json:"id"`
		Number      string `json:"number"`
		IssuedOn    string `json:"issued_on"`
		Status      string `json:"status"`
		Country     string `json:"country"`
		Environment string `json:"environment"`
		TotalCents  int64  `json:"total_cents"`
		Recipient   struct {
			LegalName string `json:"legal_name"`
		} `json:"recipient"`
	} `json:"data"`
	Pagination struct {
		Total int `json:"total"`
	} `json:"pagination"`
}

// TestInvoiceListIsNewestFirstWithTestBadge: the list shows the printed
// number, the Recipient, the total, the status and the environment (the Test
// badge is the environment field), newest first.
func TestInvoiceListIsNewestFirstWithTestBadge(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	first := issueOK(t, sessionID, validInvoiceBody())
	second := issueOK(t, sessionID, validInvoiceBody())

	list := getInvoiceList(t, sessionID)
	if list.Pagination.Total != 2 {
		t.Fatalf("total=%d, want 2", list.Pagination.Total)
	}
	if len(list.Data) != 2 {
		t.Fatalf("rows=%d, want 2", len(list.Data))
	}
	// Newest first: the second-issued invoice leads.
	if list.Data[0].ID != second.ID || list.Data[1].ID != first.ID {
		t.Fatalf("order = [%s, %s], want newest (%s) first", list.Data[0].ID, list.Data[1].ID, second.ID)
	}
	if list.Data[0].Number != "001-001-000000002" {
		t.Fatalf("newest number=%q, want 001-001-000000002", list.Data[0].Number)
	}
	if list.Data[0].Environment != "test" {
		t.Fatalf("environment=%q, want test (the Test badge)", list.Data[0].Environment)
	}
	if list.Data[0].Country != "ec" || list.Data[0].TotalCents != 11500 {
		t.Fatalf("row = %+v", list.Data[0])
	}
}

func getInvoiceList(t *testing.T, sessionID string) invoiceListView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var list invoiceListView
	if err := json.Unmarshal(env.Data, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return list
}
