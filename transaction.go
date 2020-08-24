package surebankltd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
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

func getTransactionByReceiptNumber(ctx context.Context, receiptNo string, client *firestore.Client) (*Transaction, error) {
	docSnap, err := client.Collection("transaction").Doc(receiptNo).Get(ctx)
	if err != nil {
		return nil, err
	}

	var tx Transaction
	if err = docSnap.DataTo(&tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

func Deposit(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}

	var req Transaction
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	account, err := getAccountByNumber(r.Context(), req.AccountNumber, client)
	if err != nil {
		sendError(w, "Invalid account number")
		return
	}

	currentDate := timeNow()
	if account.Type != AccountTypeDS {
		m, err := create(r.Context(), req, currentDate, client)
		if err != nil {
			log.Print(err)
			sendErrorf(w, "cannot create transaction, %s", err.Error())
			return
		}
		sendResponse(w, m)
		return
	}

	if math.Mod(req.Amount, account.Target) != 0 {
		sendErrorf(w, "Amount must be a multiple of %f", account.Target)
		return
	}

	if req.Amount/account.Target > 50 {
		sendErrorf(w, "Please pay for max of 50 days at a time, one day is %.2f", account.Target)
		return
	}

	if req.PaymentMethod != "bank_deposit" {
		req.PaymentMethod = "cash"
	}

	var tx *Transaction
	amount, reqAmount := req.Amount, req.Amount
	req.Amount = account.Target
	for amount > 0 {
		tx, err = create(r.Context(), req, currentDate, client)
		if err != nil {
			sendErrorf(w, "Cannot create transaction, %s", err.Error())
			return
		}
		amount -= account.Target
		currentDate = currentDate.Add(4 * time.Second)
	}

	customer, err := getCustomerByID(r.Context(), req.CustomerID, client)
	if err != nil {
		log.Println(err)
	}
	if serr := notify.Send(r.Context(), customer.PhoneNumber, "sms/ds_received",
		map[string]interface{}{
			"Name":          customer.Name,
			"EffectiveDate": time.Unix(tx.EffectiveDate, 0).Format("01/01/2006"),
			"Amount":        reqAmount,
			"Balance":       account.Balance + reqAmount,
		}); err != nil {
		// TODO: log critical error. Send message to monitoring account
		fmt.Println(serr)
	}
	sendResponse(w, tx)
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
		// commission count
		commissionCountRef := client.Doc(fmt.Sprintf("stats/commission/count"))
		commissionCounter, err := initCounter(ctx, 10, commissionCountRef)
		if err != nil {
			return nil, fmt.Errorf("cannot initialize commission count stat, %s", err.Error())
		}
		batch = commissionCounter.incrementCounter(ctx, commissionCountRef, 1, batch)
		// commission total
		commissionTotalRef := client.Doc(fmt.Sprintf("stats/commission/total"))
		commissionTotalCounter, err := initCounter(ctx, 10, commissionTotalRef)
		if err != nil {
			return nil, fmt.Errorf("cannot initialize commission total stat, %s", err.Error())
		}
		batch = commissionTotalCounter.incrementCounter(ctx, commissionTotalRef, commission.Amount, batch)
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
		{Path: "RecentTransactions", Value: account.RecentTransactions},
	})
	globalBalance := req.Amount * -1
	if req.Type == TransactionType_Deposit {
		globalBalance *= -1
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

	// global balance
	txTotalRef := client.Doc(fmt.Sprintf("stats/globalBalance/%s", account.Type))
	tsTotal, err := initCounter(ctx, 10, txTotalRef)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize transaction total stat, %s", err.Error())
	}
	batch = tsTotal.incrementCounter(ctx, txTotalRef, globalBalance, batch)

	if _, err = batch.Commit(ctx); err != nil {
		return nil, err
	}

	return &req, nil
}

// Withdraw inserts a new withdrawal transaction into the database.
func Withdraw(w http.ResponseWriter, r *http.Request) {

	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}

	var req WithdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	createReq := MakeDeductionRequest{
		AccountNumber: req.AccountNumber,
		Amount:        req.Amount,
		Narration:     fmt.Sprintf("%s - %s", req.PaymentMethod, req.Narration),
		SalesRep:      req.SalesRep,
		SalesRepID:    req.SalesRepID,
	}
	if req.PaymentMethod == "Transfer" {
		if len(req.Narration) > 0 {
			createReq.Narration += " -"
		}
		if len(req.Bank) > 0 && len(req.BankAccountNumber) > 0 {
			createReq.Narration += fmt.Sprintf("%s - %s", req.Bank, req.BankAccountNumber)
		}

	}

	txn, err := makeDeduction(r.Context(), createReq, timeNow(), client)
	if err != nil {
		sendError(w, err.Error())
		return
	}
	sendResponse(w, txn)
}

// MakeDeduction inserts a new transaction of type withdrawal into the database.
func makeDeduction(ctx context.Context, req MakeDeductionRequest,
	now time.Time, client *firestore.Client) (*Transaction, error) {

	account, err := getAccountByNumber(ctx, req.AccountNumber, client)
	if err != nil {
		return nil, errors.New("invalid account number")
	}

	if account.Balance < req.Amount {
		return nil, errors.New("insufficient fund")
	}

	receiptNo, err := generateReceiptNumber(ctx, client)

	m := Transaction{
		AccountNumber: account.Number,
		Type:          TransactionType_Withdrawal,
		Amount:        req.Amount,
		Narration:     req.Narration,
		SalesRepID:    req.SalesRepID,
		SalesRep:      req.SalesRep,
		CustomerID:    account.CustomerID,
		CustomerName:  account.Customer,
		ReceiptNo:     receiptNo,
		CreatedAt:     now.Unix(),
		UpdatedAt:     now.Unix(),
	}

	batch := client.Batch()

	batch = batch.Create(client.Doc("transaction/"+receiptNo), m)
	account.Balance -= req.Amount
	batch = batch.Update(client.Doc("account/"+req.AccountNumber), []firestore.Update{
		{Path: "Balance", Value: account.Balance},
	})

	if _, err := batch.Commit(ctx); err != nil {
		return nil, err
	}

	customer, err := getCustomerByID(ctx, account.CustomerID, client)
	if err != nil {
		return nil, err
	}
	if err = notify.Send(ctx, customer.PhoneNumber, "sms/payment_withdrawn",
		map[string]interface{}{
			"Name":    customer.Name,
			"Amount":  req.Amount,
			"Balance": account.Balance,
		}); err != nil {
		fmt.Println(err)
	}

	return &m, nil
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

// Archive soft deleted the transaction from the database.
func ArchiveTransaction(w http.ResponseWriter, r *http.Request) {
	currentDate := timeNow()
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}

	var req ArchiveTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	tranx, err := getTransactionByReceiptNumber(r.Context(), req.ID, client)
	if err != nil {
		log.Println(err)
		sendError(w, "cannot read transaction, please check the receipt number")
		return
	}

	if tranx.ArchivedAt > 0 {
		sendError(w, "This transaction has been archived")
		return
	}

	batch := client.Batch().Update(client.Doc("transaction/"+req.ID), []firestore.Update{{Path: "", Value: currentDate.Unix()}})

	var txAmount = tranx.Amount
	if tranx.Type == TransactionType_Deposit {
		txAmount *= -1
	}
	account, err := getAccountByNumber(r.Context(), tranx.AccountNumber, client)
	if err != nil {
		sendError(w, "cannot read error")
		return
	}
	accountRef := client.Doc("account/" + tranx.AccountNumber)
	batch = batch.Update(accountRef, []firestore.Update{
		{Path: "Balance", Value: account.Balance},
	})

	globalBalance := tranx.Amount
	if tranx.Type == TransactionType_Deposit {
		globalBalance *= -1
		// deposit count
		today := now.New(time.Unix(tranx.CreatedAt, 0))
		dailySummaryRef := client.Doc(fmt.Sprintf("dailySummary/%d", today.Unix()))
		batch = batch.Update(dailySummaryRef, []firestore.Update{{Path: "Income", Value: firestore.Increment(-1 * tranx.Amount)}})

		tsCountRef := client.Doc(fmt.Sprintf("stats/transaction/%d/%s/count", today.Unix(), tranx.Type))
		tsCounter, err := initCounter(r.Context(), 10, tsCountRef)
		if err != nil {
			sendErrorf(w, "cannot initialize transaction count stat, %s", err.Error())
			return
		}
		batch = tsCounter.incrementCounter(r.Context(), tsCountRef, -1, batch)
		// deposit total
		txTotalRef := client.Doc(fmt.Sprintf("stats/transaction/%d/%s/total", today.Unix(), tranx.Type))
		tsTotal, err := initCounter(r.Context(), 10, txTotalRef)
		if err != nil {
			sendErrorf(w, "cannot initialize transaction total stat, %s", err.Error())
			return
		}
		batch = tsTotal.incrementCounter(r.Context(), txTotalRef, tranx.Amount*-1, batch)
		// reps stat
		repStatRef := client.Doc(fmt.Sprintf("stats/transaction/%d/%s/%s", today.Unix(), tranx.SalesRepID, tranx.PaymentMethod))
		repStat, err := initCounter(r.Context(), 10, repStatRef)
		if err != nil {
			sendErrorf(w, "cannot initialize transaction reps stat, %s", err.Error())
		}
		batch = repStat.incrementCounter(r.Context(), repStatRef, tranx.Amount*-1, batch)
	}

	// global balance
	txTotalRef := client.Doc(fmt.Sprintf("stats/globalBalance/%s", account.Type))
	tsTotal, err := initCounter(r.Context(), 10, txTotalRef)
	if err != nil {
		sendErrorf(w, "cannot initialize transaction total stat, %s", err.Error())
	}
	batch = tsTotal.incrementCounter(r.Context(), txTotalRef, globalBalance, batch)

	if _, err = batch.Commit(r.Context()); err != nil {
		sendErrorf(w, "error in committing transaction, %s", err.Error())
		return
	}

	sendResponse(w, true)
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

// ArchiveTransactionRequest defines the information needed to archive a deposit. This will archive (soft-delete) the
// existing database entry.
type ArchiveTransactionRequest struct {
	ID string `json:"id" validate:"required,uuid" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86"`
}

// WithdrawRequest contains information needed to make a new Transaction.
type WithdrawRequest struct {
	Type              TransactionType `json:"type" validate:"required,oneof=deposit withdrawal"`
	AccountNumber     string          `json:"account_number" validate:"required"`
	Amount            float64         `json:"amount" validate:"required,gt=0"`
	PaymentMethod     string          `json:"payment_method" validate:"required"`
	Bank              string          `json:"bank"`
	BankAccountNumber string          `json:"bank_account_number"`
	Narration         string          `json:"narration"`
	SalesRepID        string          `json:"sales_rep_id"`
	SalesRep          string          `json:"sales_rep"`
}

type MakeDeductionRequest struct {
	AccountNumber string  `json:"account_number" validate:"required"`
	Amount        float64 `json:"amount" validate:"required,gt=0"`
	Narration     string  `json:"narration"`
	SalesRepID    string  `json:"sales_rep_id"`
	SalesRep      string  `json:"sales_rep"`
}

// DailySummary is an object representing the database table.
type DailySummary struct {
	Income      float64 `boil:"income" json:"income" toml:"income" yaml:"income"`
	Expenditure float64 `boil:"expenditure" json:"expenditure" toml:"expenditure" yaml:"expenditure"`
	BankDeposit float64 `boil:"bank_deposit" json:"bank_deposit" toml:"bank_deposit" yaml:"bank_deposit"`
	Date        int64   `boil:"date" json:"date" toml:"date" yaml:"date"`
}
