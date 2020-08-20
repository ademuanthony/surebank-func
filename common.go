package surebankltd

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type response struct {
	Data interface{} `json"data"`
	Message string `json"message"`
	Success bool `json:"success"`
}

func sendError(w http.ResponseWriter, err string)  {
	write(w, response{Message: err})
}

func sendResponse(w http.ResponseWriter, data interface{})  {
	write(w, response{Success: true, Data: data})
}

func write(w http.ResponseWriter, data response)  {
	if err := json.NewEncoder(w).Encode(data); err != nil {
		fmt.Errorf("Error in sending response, %s", err.Error())
	}
}