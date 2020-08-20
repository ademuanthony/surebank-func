package surebankltd

import (
	"encoding/json"
	"fmt"
	"net/http"
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

func sendError(w http.ResponseWriter, err string) {
	write(w, response{Message: err})
}

func sendResponse(w http.ResponseWriter, data interface{}) {
	write(w, response{Success: true, Data: data})
}

func write(w http.ResponseWriter, data response) {
	w.Header().Set("Content-Type", MIMEApplicationJSONCharsetUTF8)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fmt.Errorf("Error in sending response, %s", err.Error())
	}
}
