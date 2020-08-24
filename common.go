package surebankltd

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

const (
	charsetUTF8                    = "charset=UTF-8"
	MIMEApplicationJSON            = "application/json"
	MIMEApplicationJSONCharsetUTF8 = MIMEApplicationJSON + "; " + charsetUTF8
)

type response struct {
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
	Success bool        `json:"success"`
}

type pagedResponse struct {
	Data       interface{} `json:"data"`
	TotalCount int64       `json:"total_count"`
	Message    string      `json:"message"`
	Success    bool        `json:"success"`
}

func sendError(w http.ResponseWriter, err string) {
	write(w, response{Message: err})
}

func sendResponse(w http.ResponseWriter, data interface{}) {
	write(w, response{Success: true, Data: data})
}

func sendPagedResponse(w http.ResponseWriter, data interface{}, totalCount int64) {
	write(w, pagedResponse{Success: true, Data: data, TotalCount: totalCount})
}

func write(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", MIMEApplicationJSONCharsetUTF8)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Fatalf("Error in sending response, %s", err.Error())
	}
}

func timeNow() time.Time {
	return time.Now().UTC().Add(1 * time.Hour)
}

type FindByIdRequest struct {
	ID string `json:"id"`
}

type documentCount struct {
	Count int
}

type txStat struct {
	Count int
	Total float64
	Bank  float64
	Cash  float64
}
