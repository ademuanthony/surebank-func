package surebankltd

// DsCommission represents a Commission that is returned for display.
type DSCommission struct {
	ID            string  `json:"id" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86" truss:"api-read"`
	AccountNumber string  `json:"account_number" example:"SB10003001" truss:"api-read"`
	CustomerID    string  `json:"customer_id" truss:"api-read"`
	CustomerName  string  `json:customer_name" truss:"api-read"`
	Amount        float64 `json:"amount" truss:"api-read"`
	Date          int64   `json:"date" truss:"api-read"`
	EffectiveDate int64   `json:"effective_date" truss:"api-read"`
}
