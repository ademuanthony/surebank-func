package surebankltd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/ademuanthony/surebankltd/notify"
	"github.com/jinzhu/now"
	"github.com/pborman/uuid"
	"github.com/pkg/errors"
)

// ChecklistStatus values define the status field of checklist.
const (
	// TransactionType_Deposit defines the type of deposit transaction.
	TransactionType_Deposit TransactionType = "deposit"
	// TransactionType_Withdrawal defines the type of withdrawal transaction.
	TransactionType_Withdrawal TransactionType = "withdrawal"

	PaymentMethod_Cash string = "cash"
	PaymentMethod_Bank string = "bank_deposit"
)

// CreateTransactionHTTP is an HTTP Cloud Function with a request parameter.
func CreateTransactionHTTP(w http.ResponseWriter, r *http.Request) {
	var d Transaction
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		fmt.Fprint(w, "Hello, World!")
		return
	}

}

func create(ctx context.Context, req Transaction, currentDate time.Time, client *firestore.Client) (*Transaction, error) {

	account, err := getAccountByNumber(ctx, req.AccountNumber, client)
	if err != nil {
		log.Println(err)
		return nil, errors.New("cannot map account data")
	}

	var customer Customer
	customerRef := client.Doc("customer/" + account.CustomerID)
	customerSnap, err := customerRef.Get(ctx)
	if err != nil {
		return nil, errors.Errorf("cannot read customer data, %s", err.Error())
	}
	if err = customerSnap.DataTo(&customer); err != nil {
		return nil, errors.New("cannot map customer data")
	}

	// If now empty set it to the current time.
	if currentDate.IsZero() {
		currentDate = timeNow()
	}

	today := now.New(currentDate).BeginningOfDay()
	effectiveDate := today
	if account.Type == AccountTypeDS {
		if len(account.RecentTransactions) > 0 {
			effectiveDate = now.New(time.Unix(account.RecentTransactions[0].EffectiveDate, 0)).Time.Add(24 * time.Hour)
		}
	}

	isFirstContribution, err := startingNewCircle(account.LastCommissionDate, effectiveDate)
	if err != nil {
		return nil, err
	}

	if req.Type == TransactionType_Deposit {
		account.LastPaymentDate = effectiveDate.Unix()
		account.Balance += req.Amount
	} else {
		account.Balance -= req.Amount
	}

	receiptNumber, err := generateReceiptNumber(ctx, client)
	if err != nil {
		log.Println(err)
		return nil, fmt.Errorf("error in generating receipt number, %s", err.Error())
	}
	req.ReceiptNo = receiptNumber
	batch := client.Batch().Create(client.Doc("transaction/"+receiptNumber), req)

	dailySummaryRef := client.Doc(fmt.Sprintf("dailySummary/%d", today.Unix()))
	if _, err := dailySummaryRef.Get(ctx); err != nil {
		if _, err = dailySummaryRef.Create(ctx, DailySummary{}); err != nil {
			return nil, fmt.Errorf("cannot initialize daily summary, %s", err.Error())
		}
	}
	batch = batch.Update(dailySummaryRef, []firestore.Update{{Path: "Income", Value: firestore.Increment(req.Amount)}})

	if req.Type == TransactionType_Deposit && account.Type == AccountTypeDS && isFirstContribution {
		receiptNumber, err := generateReceiptNumber(ctx, client)
		if err != nil {
			log.Println(err)
			return nil, fmt.Errorf("error in generating receipt number, %s", err.Error())
		}
		wm := Transaction{
			ReceiptNo:     receiptNumber,
			AccountNumber: account.Number,
			Amount:        req.Amount,
			Narration:     "DS fee deduction",
			Type:          TransactionType_Withdrawal,
			SalesRepID:    req.SalesRepID,
			SalesRep:      req.SalesRep,
			CustomerID:    req.CustomerID,
			CustomerName:  req.CustomerName,
			CreatedAt:     currentDate.Add(2 * time.Second).Unix(),
			UpdatedAt:     currentDate.Unix(),
		}
		batch = batch.Create(client.Doc("transaction/"+receiptNumber), wm)
		account.Balance -= req.Amount

		commission := DSCommission{
			ID:            uuid.NewRandom().String(),
			AccountNumber: account.Number,
			CustomerID:    account.CustomerID,
			CustomerName:  req.CustomerName,
			Amount:        req.Amount,
			Date:          currentDate.Unix(),
			EffectiveDate: effectiveDate.Unix(),
		}
		account.LastCommissionDate = commission.EffectiveDate
		batch = batch.Create(client.Doc("commission/"+commission.ID), commission)
	}

	// send SMS notification
	if req.Type == TransactionType_Deposit {
		if account.Type == AccountTypeSB {
			if err = notify.Send(ctx, customer.PhoneNumber, "sms/payment_received",
				map[string]interface{}{
					"Name":    customer.Name,
					"Amount":  req.Amount,
					"Balance": account.Balance,
				}); err != nil {
				// TODO: log critical error. Send message to monitoring account
				fmt.Println(err)
			}
		}
	} else {
		if err = notify.Send(ctx, customer.PhoneNumber, "sms/payment_withdrawn",
			map[string]interface{}{
				"Name":    customer.Name,
				"Amount":  req.Amount,
				"Balance": account.Balance,
			}); err != nil {
			// TODO: log critical error. Send message to monitoring account
			fmt.Println(err)
		}
	}

	if len(account.RecentTransactions) >= 5 {
		account.RecentTransactions = account.RecentTransactions[:len(account.RecentTransactions)-1]
	}
	account.RecentTransactions = append([]Transaction{req}, account.RecentTransactions...)
	accountRef := client.Doc("account/" + req.AccountNumber)
	batch = batch.Update(accountRef, []firestore.Update{
		{Path: "Balance", Value: account.Balance},
		{Path: "LastPaymentDate", Value: effectiveDate.Unix()},
		{Path: "LastCommissionDate", Value: account.LastCommissionDate},
	})
	// increase deposit count and total
	if req.Type == TransactionType_Deposit {
		// deposit count
		tsCountRef := client.Doc(fmt.Sprintf("stats/transaction/%d/%s/count", today.Unix(), req.Type))
		tsCounter, err := initCounter(ctx, 10, tsCountRef)
		if err != nil {
			return nil, fmt.Errorf("cannot initialize transaction count stat, %s", err.Error())
		}
		batch = tsCounter.incrementCounter(ctx, tsCountRef, 1, batch)
		// deposit total
		txTotalRef := client.Doc(fmt.Sprintf("stats/transaction/%d/%s/total", today.Unix(), req.Type))
		tsTotal, err := initCounter(ctx, 10, txTotalRef)
		if err != nil {
			return nil, fmt.Errorf("cannot initialize transaction total stat, %s", err.Error())
		}
		batch = tsTotal.incrementCounter(ctx, txTotalRef, req.Amount, batch)
		// reps stat
		repStatRef := client.Doc(fmt.Sprintf("stats/transaction/%d/%s/%s", today.Unix(), req.SalesRepID, req.PaymentMethod))
		repStat, err := initCounter(ctx, 10, repStatRef)
		if err != nil {
			return nil, fmt.Errorf("cannot initialize transaction reps stat, %s", err.Error())
		}
		batch = repStat.incrementCounter(ctx, repStatRef, req.Amount, batch)
	}

	if _, err = batch.Commit(ctx); err != nil {
		return nil, err
	}

	return &req, nil
}

func startingNewCircle(lastCommissionDate int64, effectiveDate time.Time) (bool, error) {
	lastDate := now.New(time.Unix(lastCommissionDate, 0)).BeginningOfDay()
	effectiveDate = now.New(effectiveDate).BeginningOfDay()
	duration := effectiveDate.Sub(lastDate)
	r := duration.Hours() >= float64(31*24)
	return r, nil
}

func generateReceiptNumber(ctx context.Context, client *firestore.Client) (string, error) {
	var receipt string
	var uniqueFound bool
	for !uniqueFound {
		receipt = "TX"
		rand.Seed(time.Now().UTC().UnixNano())
		for i := 0; i < 6; i++ {
			receipt += strconv.Itoa(rand.Intn(10))
		}
		if tx, _ := readTransactionByReceiptNumber(ctx, receipt, client); tx == nil {
			uniqueFound = true
		}
	}
	return receipt, nil
}

func readTransactionByReceiptNumber(ctx context.Context, receiptNumber string, client *firestore.Client) (*Transaction, error) {
	docSnap, err := client.Collection("transaction").Doc(receiptNumber).Get(ctx)
	if err != nil {
		return nil, err
	}

	var tx Transaction
	if err = docSnap.DataTo(&tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

type TransactionType string

type Transaction struct {
	ReceiptNo     string          `json:"receipt_no"`
	Type          TransactionType `json:"tx_type,omitempty" example:"deposit"`
	AccountNumber string          `json:"account_number" example:"SB10003001" truss:"api-read"`
	CustomerID    string          `json:"customer_id" truss:"api-read"`
	CustomerName  string          `json:"customer_name" truss:"api-read"`
	Amount        float64         `json:"amount" truss:"api-read"`
	Narration     string          `json:"narration" truss:"api-read"`
	PaymentMethod string          `json:"payment_method" truss:"api-read"`
	SalesRepID    string          `json:"sales_rep_id" truss:"api-read"`
	SalesRep      string          `json:"sales_rep,omitempty" truss:"api-read"`
	EffectiveDate int64           `json:"effective_date" truss:"api-read"`
	CreatedAt     int64           `json:"created_at" truss:"api-read"`            // CreatedAt contains multiple format options for display.
	UpdatedAt     int64           `json:"updated_at" truss:"api-read"`            // UpdatedAt contains multiple format options for display.
	ArchivedAt    int64           `json:"archived_at,omitempty" truss:"api-read"` // ArchivedAt contains multiple format options for display.
}

// DailySummary is an object representing the database table.
type DailySummary struct {
	Income      float64 `boil:"income" json:"income" toml:"income" yaml:"income"`
	Expenditure float64 `boil:"expenditure" json:"expenditure" toml:"expenditure" yaml:"expenditure"`
	BankDeposit float64 `boil:"bank_deposit" json:"bank_deposit" toml:"bank_deposit" yaml:"bank_deposit"`
	Date        int64   `boil:"date" json:"date" toml:"date" yaml:"date"`
}
