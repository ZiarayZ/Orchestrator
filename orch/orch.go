package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

var wordpressPort string
var regularPort string
var password string

type Orchestrator struct {
	URL      string
	Platform string
	Check    []string
}

func orch_handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Orch-Token") == password {
		logger := r.Context().Value("RequestLogger").(*logrus.Entry)

		//enforce limits
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		var orch Orchestrator

		err := dec.Decode(&orch)
		if err != nil {
			logger.Infof("Request Failed. " + err.Error())
			var syntaxError *json.SyntaxError
			var unmarshalTypeError *json.UnmarshalTypeError

			switch {
			// Catch JSON syntax errors
			case errors.As(err, &syntaxError):
				msg := fmt.Sprintf("Request body contains badly-formed JSON (at position %d)", syntaxError.Offset)
				http.Error(w, msg, http.StatusBadRequest)
			// Catch these errors when Decode returns an unexpected EOF
			case errors.Is(err, io.ErrUnexpectedEOF):
				msg := fmt.Sprintf("Request body contains badly-formed JSON")
				http.Error(w, msg, http.StatusBadRequest)
			// Catch any type errors
			case errors.As(err, &unmarshalTypeError):
				msg := fmt.Sprintf("Request body contains an invalid value for the %q field (at position %d)", unmarshalTypeError.Field, unmarshalTypeError.Offset)
				http.Error(w, msg, http.StatusBadRequest)
			// Catch errors caused by extra unexpected fields
			case strings.HasPrefix(err.Error(), "json: unknown field "):
				fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
				msg := fmt.Sprintf("Request body contains unknown field %s", fieldName)
				http.Error(w, msg, http.StatusBadRequest)
			// An io.EOF error is returned by Decode() if the request body is empty
			case errors.Is(err, io.EOF):
				msg := "Request body must not be empty"
				http.Error(w, msg, http.StatusBadRequest)
			// Catch the error caused by the request body being too large
			case err.Error() == "http: request body too large":
				msg := "Request body must not be larger than 1MB"
				http.Error(w, msg, http.StatusRequestEntityTooLarge)
			// Otherwise default to logging the error and sending a 500 Internal Server Error response.
			default:
				logger.Infof(err.Error())
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Catch if there's multiple JSON objects
		err = dec.Decode(&struct{}{})
		if err != io.EOF {
			logger.Infof("Request Failed. EOF err")
			msg := "Request body must only contain a single JSON object"
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		//log the information sent
		logger.Infof("%v", orch)

		//make request to wordpress/regular component
		if orch.Platform == "wordpress" || orch.Platform == "regular" {
			newBody, err := json.Marshal(orch)
			if err != nil {
				logger.Infof("Encode JSON: " + err.Error())
				http.Error(w, "Encode JSOn Failed.", http.StatusBadRequest)
				return
			}
			newPort := regularPort
			//edit ports
			if orch.Platform == "wordpress" {
				newPort = "4001"
			} else if orch.Platform == "regular" {
				newPort = "4002"
			} else {
				logger.Infof("Requested Site Type Error: " + orch.Platform)
				http.Error(w, "Requested Site Type Error: "+orch.Platform, http.StatusBadRequest)
				return
			}
			req, err := http.NewRequest("POST", "http://localhost:"+newPort+"/"+orch.Platform, bytes.NewBuffer(newBody))
			if err != nil {
				logger.Infof("Request Creation: " + err.Error())
				http.Error(w, "Request Creation Failed.", http.StatusBadRequest)
				return
			}
			//cheating and converting to string
			for k, v := range logger.Data {
				if k == "correlationID" {
					corrID := fmt.Sprintf("%v", v.(uuid.UUID))
					req.Header.Add("Correlation-ID", corrID) //get the uuid
				}
			}
			req.Header.Set("Content-Type", "application/json")
			//relay auth token if it's for wordpress
			if orch.Platform == "wordpress" {
				req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
				req.Header.Set("Cookie", r.Header.Get("Cookie"))
			}
			//send request and receive response
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				logger.Infof("Request Sent: " + err.Error())
				http.Error(w, "Request Sent Failed.", http.StatusBadRequest)
				return
			}
			defer resp.Body.Close()
			//get string
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				logger.Infof("Read Response: " + err.Error())
				http.Error(w, "Read Response Failed.", http.StatusBadRequest)
				return
			}
			//return string, either ok or error
			logger.Infof("Status OK")
			fmt.Fprintf(w, string(b))
		} else {
			logger.Infof("Incorrect Platform.")
			http.Error(w, "Incorrect platform.", http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, "Invalid Request Token.", http.StatusBadRequest)
		return
	}
}

//create the correlation ID and attach it to the logger
func CorrelationGeneration(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New()
		entry := logrus.WithFields(logrus.Fields{
			"correlationID": id,
		})
		ctx := context.WithValue(r.Context(), "RequestLogger", entry)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	r := mux.NewRouter()
	r.Use(CorrelationGeneration)
	r.HandleFunc("/orch", orch_handle)

	port := "4000"
	wordpressPort = "4001"
	regularPort = "4002"
	// replace "uuid.New().String()"" with your own token string if wanted/needed
	password = uuid.New().String()
	fmt.Print(password)
	err := http.ListenAndServe(":"+port, r)
	log.Fatal(err)
}
