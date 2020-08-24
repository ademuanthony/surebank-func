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
	"github.com/pborman/uuid"
	"google.golang.org/api/iterator"
)

const (
	AccountTypeDS = "DS"
	AccountTypeSB = "SB"
)

// CreateCustomerHTTP is an HTTP Cloud Function for creating a customer
func CreateCustomerHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}
	if req.Name == "" {
		sendError(w, "name is required")
		return
	}
	if req.PhoneNumber == "" {
		sendError(w, "phone number is required")
		return
	}

	customerCollection := client.Collection("customer")
	now := timeNow()
	m := Customer{
		ID:          uuid.NewRandom().String(),
		Email:       req.Email,
		Name:        req.Name,
		PhoneNumber: req.PhoneNumber,
		Address:     req.Address,
		SalesRepID:  req.SalesRepID,
		CreatedAt:   now.Unix(),
		BranchID:    req.BranchID,
		UpdatedAt:   now.Unix(),
		Branch:      req.Branch,
		SalesRep:    req.SalesRep,
		ShortName:   req.ShortName,
	}

	accountNumber, err := generateAccountNumber(r.Context(), client, req.Type)
	if err != nil {
		sendError(w, fmt.Sprintf("cannot generate account number, %s", err.Error()))
		return
	}

	account := Account{
		Number:     accountNumber,
		BranchID:   req.BranchID,
		Branch:     req.Branch,
		CustomerID: m.ID,
		Customer:   m.Name,
		SalesRep:   req.SalesRep,
		SalesRepID: req.SalesRepID,
		Target:     req.Target,
		TargetInfo: req.TargetInfo,
		Type:       req.Type,
	}

	customerStat := client.Doc("stats/customer")
	customerCounter, err := initCounter(r.Context(), 10, customerStat)
	if err != nil {
		log.Println(err)
		sendError(w, "Cannot init customer count")
	}
	batch := customerCounter.incrementCounter(r.Context(), customerStat, 1, client.Batch())

	accountStat := client.Doc("stats/account")
	accountCounter, err := initCounter(r.Context(), 10, accountStat)
	if err != nil {
		log.Println(err)
		sendError(w, "Cannot init account count")
	}
	batch = accountCounter.incrementCounter(r.Context(), accountStat, 1, batch)

	if _, err := batch.
		Create(customerCollection.Doc(m.ID), m).
		Update(customerStat, []firestore.Update{{Path: "Count", Value: firestore.Increment(1)}}).
		Create(client.Doc("account/"+accountNumber), account).
		Update(accountStat, []firestore.Update{{Path: "Count", Value: firestore.Increment(1)}}).
		Commit(r.Context()); err != nil {
		sendError(w, err.Error())
		return
	}

	sendResponse(w, m)
}

func ListCustomerHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	var query firestore.Query = client.Collection("customer").OrderBy("CreatedAt", firestore.Desc)
	if req.Limit > 0 {
		query = query.Limit(req.Limit)
	}
	if req.Offset > 0 {
		query = query.Offset(req.Offset)
	}
	if req.SalesRepID != "" {
		query = query.Where("SalesRepID", "==", req.SalesRepID)
	}

	var customers []Customer
	iter := query.Documents(r.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Println(err)
			sendError(w, "cannot read customer data")
			return
		}
		var c Customer
		if err = doc.DataTo(&c); err != nil {
			log.Println(err)
			sendError(w, "cannot read customer data")
			return
		}
		customers = append(customers, c)
	}

	customerStatRef := client.Doc("stats/customer")
	totalCount, err := getCount(r.Context(), customerStatRef)
	if err != nil {
		sendError(w, "cannot get the total count of customers")
		log.Println(err)
		return
	}
	sendPagedResponse(w, customers, totalCount)
}

func FindCustomerByIdHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindByIdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	docSnap, err := client.Collection("customer").Doc(req.ID).Get(r.Context())
	if err != nil {
		sendError(w, "customer not found")
		return
	}

	var customer Customer
	if err = docSnap.DataTo(&customer); err != nil {
		sendError(w, "cannot map customer data")
		return
	}

	sendResponse(w, customer)
}

// CreateAccountHTTP is an HTTP Cloud Function for creating an account
func CreateAccountHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req Account
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}
	if req.CustomerID == "" {
		sendError(w, "customer ID is required")
		return
	}
	if req.Type == "" {
		sendError(w, "account type is required is required")
		return
	}

	accountNumber, err := generateAccountNumber(r.Context(), client, req.Type)
	if err != nil {
		sendError(w, fmt.Sprintf("cannot generate account number, %s", err.Error()))
		return
	}

	req.Number = accountNumber
	now := timeNow()
	req.CreatedAt = now.Unix()
	req.UpdatedAt = now.Unix()

	accountStat := client.Doc("stats/account")
	if _, err := accountStat.Get(r.Context()); err != nil {
		_, err = accountStat.Set(r.Context(), documentCount{})
		if err != nil {
			log.Println(err)
			sendError(w, "Cannot init account count")
		}
	}

	if _, err := client.Batch().
		Create(client.Doc("account/"+accountNumber), req).
		Update(accountStat, []firestore.Update{{Path: "Count", Value: firestore.Increment(1)}}).
		Commit(r.Context()); err != nil {
		sendError(w, err.Error())
		return
	}

	sendResponse(w, req)
}

func ListAccountHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	var query firestore.Query = client.Collection("account").OrderBy("CreatedAt", firestore.Desc)
	if req.Limit > 0 {
		query = query.Limit(req.Limit)
	}
	if req.Offset > 0 {
		query = query.Offset(req.Offset)
	}
	if req.SalesRepID != "" {
		query = query.Where("SalesRepID", "==", req.SalesRepID)
	}

	var accounts []Account
	iter := query.Documents(r.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Println(err)
			sendError(w, "cannot read account data")
			return
		}
		var c Account
		if err = doc.DataTo(&c); err != nil {
			log.Println(err)
			sendError(w, "cannot read account data")
			return
		}
		accounts = append(accounts, c)
	}

	customerStatRef := client.Doc("stats/account")
	totalCount, err := getCount(r.Context(), customerStatRef)
	if err != nil {
		sendError(w, "cannot get the total count of accounts")
		log.Println(err)
		return
	}
	sendPagedResponse(w, accounts, totalCount)
}

func ListDSAccountHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	var query firestore.Query = client.Collection("account").Where("Type", "==", "DS").Where("Balance", ">", 0).
		OrderBy("CreatedAt", firestore.Desc)
	if req.Limit > 0 {
		query = query.Limit(req.Limit)
	}
	if req.Offset > 0 {
		query = query.Offset(req.Offset)
	}
	if req.SalesRepID != "" {
		query = query.Where("SalesRepID", "==", req.SalesRepID)
	}

	var accounts []Account
	iter := query.Documents(r.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Println(err)
			sendError(w, "cannot read account data")
			return
		}
		var c Account
		if err = doc.DataTo(&c); err != nil {
			log.Println(err)
			sendError(w, "cannot read account data")
			return
		}
		accounts = append(accounts, c)
	}

	customerStatRef := client.Doc("stats/account")
	totalCount, err := getCount(r.Context(), customerStatRef)
	if err != nil {
		sendError(w, "cannot get the total count of accounts")
		log.Println(err)
		return
	}

	sendPagedResponse(w, accounts, totalCount)
}

func ListDebtorsHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	currentDate := time.Now()
	threeDaysAgo := currentDate.Add(-3 * 24 * time.Hour).Unix()
	thirtyDaysAgo := currentDate.Add(-30 * 24 * time.Hour).Unix()

	var query firestore.Query = client.Collection("account").Where("Type", "==", "DS").
		Where("LastPaymentDate", ">=", thirtyDaysAgo).
		Where("LastPaymentDate", "<=", threeDaysAgo).
		OrderBy("LastPaymentDate", firestore.Asc)
	if req.Limit > 0 {
		query = query.Limit(req.Limit)
	}
	if req.Offset > 0 {
		query = query.Offset(req.Offset)
	}
	if req.SalesRepID != "" {
		query = query.Where("SalesRepID", "==", req.SalesRepID)
	}

	var accounts []Account
	iter := query.Documents(r.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Println(err)
			sendError(w, "cannot read account data")
			return
		}
		var c Account
		if err = doc.DataTo(&c); err != nil {
			log.Println(err)
			sendError(w, "cannot read account data")
			return
		}
		accounts = append(accounts, c)
	}

	customerStatRef := client.Doc("stats/account")
	totalCount, err := getCount(r.Context(), customerStatRef)
	if err != nil {
		sendError(w, "cannot get the total count of accounts")
		log.Println(err)
		return
	}

	sendPagedResponse(w, accounts, totalCount)
}

func FindAccountByIdHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Println(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindByIdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println(err)
		sendError(w, "cannot decode client request")
		return
	}

	account, err := getAccountByNumber(r.Context(), req.ID, client)
	if err != nil {
		log.Println(err)
		sendError(w, "Cannot read account by the specified number")
		return
	}

	sendResponse(w, account)
}

func getAccountByNumber(ctx context.Context, accountNumber string, client *firestore.Client) (*Account, error) {
	docSnap, err := client.Collection("account").Doc(accountNumber).Get(ctx)
	if err != nil {
		return nil, err
	}

	var account Account
	if err = docSnap.DataTo(&account); err != nil {
		return nil, err
	}
	return &account, nil
}

func generateAccountNumber(ctx context.Context, client *firestore.Client, accountType string) (string, error) {
	var accountNumber string
	var unique bool
	for !unique {
		accountNumber = accountType
		rand.Seed(time.Now().UTC().UnixNano())
		for i := 0; i < 5; i++ {
			accountNumber += strconv.Itoa(rand.Intn(10))
		}
		_, err := client.Doc("account/" + accountNumber).Get(ctx)
		if err != nil {
			unique = true
		}
	}
	return accountNumber, nil
}

// Customer represents a workflow.
type Customer struct {
	ID          string `json:"id" validate:"required,uuid" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86"`
	Name        string `json:"name"  validate:"required" example:"Rocket Launch"`
	ShortName   string `json:"short_name"`
	Email       string `json:"email" truss:"api-read"`
	PhoneNumber string `json:"phone_number" truss:"api-read"`
	Address     string `json:"address" truss:"api-read"`
	SalesRepID  string `json:"sales_rep_id" truss:"api-read"`
	BranchID    string `json:"branch_id" truss:"api-read"`
	SalesRep    string `json:"sales_rep" truss:"api-read"`
	Branch      string `json:"branch" truss:"api-read"`
	CreatedAt   int64  `json:"created_at" truss:"api-read"`
	UpdatedAt   int64  `json:"updated_at" truss:"api-read"`
	ArchivedAt  int64  `json:"archived_at,omitempty" truss:"api-hide"`
}

// FindRequest defines the possible options to search for customers. By default
// archived checklist will be excluded from response.
type FindCustomerRequest struct {
	SalesRepID string        `json:"sales_rep_id" example:"dfasf-q43-dfas-32432sdaf-adsf"`
	Args       []interface{} `json:"args" swaggertype:"array,string" example:"Moon Launch,active"`
	Order      []string      `json:"order" example:"created_at desc"`
	Limit      int           `json:"limit" example:"10"`
	Offset     int           `json:"offset" example:"20"`
}

// Account represents a customer account.
type Account struct {
	Number             string  `json:"number"  validate:"required" example:"Rocket Launch"`
	CustomerID         string  `json:"customer_id" validate:"required,uuid" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86"`
	Type               string  `json:"type" truss:"api-read"`
	Balance            float64 `json:"balance" truss:"api-read"`
	Target             float64 `json:"target" truss:"api-read"`
	TargetInfo         string  `json:"target_info" truss:"api-read"`
	SalesRepID         string  `json:"sales_rep_id" truss:"api-read"`
	BranchID           string  `json:"branch_id" truss:"api-read"`
	LastPaymentDate    int64   `json:"last_payment_date"`
	LastCommissionDate int64   `json:"last_commission"`
	CreatedAt          int64   `json:"created_at" truss:"api-read"`
	UpdatedAt          int64   `json:"updated_at" truss:"api-read"`
	ArchivedAt         int64   `json:"archived_at,omitempty" truss:"api-hide"`
	SalesRep           string  `json:"sales_rep" truss:"api-read"`
	Branch             string  `json:"branch" truss:"api-read"`
	Customer           string  `json:"customer"`

	RecentTransactions []Transaction
}

// CreateCustomerRequest contains information needed to create a new Customer.
type CreateCustomerRequest struct {
	Name        string `json:"name"  validate:"required" example:"Rocket Launch"`
	ShortName   string `json:"short_name"`
	Email       string `json:"email" truss:"api-read"`
	PhoneNumber string `json:"phone_number" truss:"api-read"`
	Address     string `json:"address" truss:"api-read"`
	SalesRepID  string `json:"sales_rep_id" truss:"api-read"`
	BranchID    string `json:"branch_id" truss:"api-read"`
	SalesRep    string `json:"sales_rep" truss:"api-read"`
	Branch      string `json:"branch" truss:"api-read"`

	Type       string  `json:"type" validate:"required"`
	Target     float64 `json:"target"`
	TargetInfo string  `json:"target_info"`
}
