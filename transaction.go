package surebankltd

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
)

// CreateTransactionHTTP is an HTTP Cloud Function with a request parameter.
func CreateTransactionHTTP(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		fmt.Fprint(w, "Hello, World!")
		return
	}
	if d.Name == "" {
		fmt.Fprint(w, "Hello, World!")
		return
	}
	fmt.Fprintf(w, "Hello, %s!", html.EscapeString(d.Name))
}

type TransactionType string

type Transaction struct {
	ID            string          `json:"id" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86" truss:"api-read"`
	AccountID     string          `json:"account_id" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86" truss:"api-read"`
	Type          TransactionType `json:"tx_type,omitempty" example:"deposit"`
	AccountNumber string          `json:"account_number" example:"SB10003001" truss:"api-read"`
	CustomerID    string          `json:"customer_id" truss:"api-read"`
	CustomerName  string          `json:"customer_name" truss:"api-read"`
	Amount        float64         `json:"amount" truss:"api-read"`
	Narration     string          `json:"narration" truss:"api-read"`
	PaymentMethod string          `json:"payment_method" truss:"api-read"`
	SalesRepID    string          `json:"sales_rep_id" truss:"api-read"`
	SalesRep      string          `json:"sales_rep,omitempty" truss:"api-read"`
	ReceiptNo     string          `json:"receipt_no"`
	EffectiveDate int64           `json:"effective_date" truss:"api-read"`
	CreatedAt     int64           `json:"created_at" truss:"api-read"`            // CreatedAt contains multiple format options for display.
	UpdatedAt     int64           `json:"updated_at" truss:"api-read"`            // UpdatedAt contains multiple format options for display.
	ArchivedAt    int64           `json:"archived_at,omitempty" truss:"api-read"` // ArchivedAt contains multiple format options for display.
}
