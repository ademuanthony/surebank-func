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

// CreateCustomerHTTP is an HTTP Cloud Function for creating a customer
func CreateCustomerHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Fatal(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Fatal(err)
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
	now := time.Now().UTC()
	m := Customer{
		ID:          uuid.NewRandom().String(),
		Email:       req.Email,
		Name:        req.Name,
		PhoneNumber: req.PhoneNumber,
		Address:     req.Address,
		SalesRepID:  req.SalesRepID,
		CreatedAt:   now,
		BranchID:    req.BranchID,
		UpdatedAt:   now,
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

	if _, err := client.Batch().Create(customerCollection.Doc(m.ID), m).
		Create(client.Doc("account/"+accountNumber), account).Commit(r.Context()); err != nil {
		sendError(w, err.Error())
		return
	}

	sendResponse(w, m)
}

func ListCustomerHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Fatal(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Fatal(err)
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
			log.Fatal(err)
			sendError(w, "cannot read customer data")
			return
		}
		var c Customer
		if err = doc.DataTo(&c); err != nil {
			log.Fatal(err)
			sendError(w, "cannot read customer data")
			return
		}
		customers = append(customers, c)
	}

	sendResponse(w, customers)
}

func FindCustomerByIdHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := firestore.NewClient(r.Context(), "surebank")
	if err != nil {
		log.Fatal(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req FindByIdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Fatal(err)
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
		log.Fatal(err)
		sendError(w, "cannot establish database connection")
		return
	}
	var req Account
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Fatal(err)
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

	if _, err := client.Batch().
		Create(client.Doc("account/"+accountNumber), req).Commit(r.Context()); err != nil {
		sendError(w, err.Error())
		return
	}

	sendResponse(w, req)
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
	ID          string     `json:"id" validate:"required,uuid" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86"`
	Name        string     `json:"name"  validate:"required" example:"Rocket Launch"`
	ShortName   string     `json:"short_name"`
	Email       string     `json:"email" truss:"api-read"`
	PhoneNumber string     `json:"phone_number" truss:"api-read"`
	Address     string     `json:"address" truss:"api-read"`
	SalesRepID  string     `json:"sales_rep_id" truss:"api-read"`
	BranchID    string     `json:"branch_id" truss:"api-read"`
	SalesRep    string     `json:"sales_rep" truss:"api-read"`
	Branch      string     `json:"branch" truss:"api-read"`
	CreatedAt   time.Time  `json:"created_at" truss:"api-read"`
	UpdatedAt   time.Time  `json:"updated_at" truss:"api-read"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty" truss:"api-hide"`
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
	Number          string     `json:"number"  validate:"required" example:"Rocket Launch"`
	CustomerID      string     `json:"customer_id" validate:"required,uuid" example:"985f1746-1d9f-459f-a2d9-fc53ece5ae86"`
	Type            string     `json:"type" truss:"api-read"`
	Balance         float64    `json:"balance" truss:"api-read"`
	Target          float64    `json:"target" truss:"api-read"`
	TargetInfo      string     `json:"target_info" truss:"api-read"`
	SalesRepID      string     `json:"sales_rep_id" truss:"api-read"`
	BranchID        string     `json:"branch_id" truss:"api-read"`
	LastPaymentDate time.Time  `json:"last_payment_date"`
	CreatedAt       time.Time  `json:"created_at" truss:"api-read"`
	UpdatedAt       time.Time  `json:"updated_at" truss:"api-read"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty" truss:"api-hide"`
	SalesRep        string     `json:"sales_rep" truss:"api-read"`
	Branch          string     `json:"branch" truss:"api-read"`
	Customer        string     `json:"customer"`
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
