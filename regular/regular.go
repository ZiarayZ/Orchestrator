package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"gopkg.in/mgo.v2"
)

//global port
var port string
var orch_token string

//mongoDB constants
const (
	hosts      = "localhost:27017" //IP
	database   = "logs"            //DB (schema)
	username   = ""                //login details
	password   = ""                //login details
	collection = "regular"         //Collection (table)
)

type Log struct {
	Correlation_ID string
	Date           interface{}
	URL            string
	Status_code    int
}

type Orchestrator struct {
	URL      string
	Platform string //ignored
	Check    []string
}

type MongoStore struct {
	session *mgo.Session
}

var mongoStore = MongoStore{}

func log_update(logVar Log, logID, logURL string, logStatus_code int, logCol *mgo.Collection, logLogger *logrus.Entry) {
	logVar.Correlation_ID = logID
	logVar.URL = logURL
	logVar.Status_code = logStatus_code
	logVar.Date = time.Now()
	err := logCol.Insert(logVar)
	if err != nil {
		panic(err)
	}
	logLogger.Infof("%v", logVar)
}

func regular_handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Orch-Token") == orch_token {
		if r.Header.Get("Correlation-ID") != "" {
			if r.Header.Get("Content-Type") != "application/json" {
				msg := "Content type should be application/json, not: " + r.Header.Get("Content-Type")
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
		} else {
			msg := "No correlation ID set"
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		logger := r.Context().Value("RequestLogger").(*logrus.Entry)
		col := mongoStore.session.DB(database).C(collection)
		var toLog Log

		//enforce limits
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		var orch Orchestrator

		err := dec.Decode(&orch)
		if err != nil {
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
			msg := "Request body must only contain a single JSON object"
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		// handle information and respond
		resp, err := http.Get("https://" + orch.URL)
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			//log it to DB
			log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, resp.StatusCode, col, logger)
			http.Error(w, orch.URL+": OK", resp.StatusCode)
			return
		} else {
			//log it to DB
			log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, resp.StatusCode, col, logger)
			http.Error(w, "Status Code Not OK", resp.StatusCode)
			return
		}
	} else {
		http.Error(w, "Invalid Request Token.", http.StatusBadRequest)
	}
}

//grab the correlation ID and attach it to the logger
func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Correlation-ID")
		entry := logrus.WithFields(logrus.Fields{
			"correlationID": id,
		})
		ctx := context.WithValue(r.Context(), "RequestLogger", entry)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func initialiseMongo() (session *mgo.Session) {
	info := &mgo.DialInfo{
		Addrs:    []string{hosts},
		Timeout:  60 * time.Second,
		Database: database,
		Username: username,
		Password: password,
	}

	session, err := mgo.DialWithInfo(info)
	if err != nil {
		panic(err)
	}

	return
}

func main() {
	//connect to mongoDB server/container
	session := initialiseMongo()
	mongoStore.session = session

	//create connection and/or endpoint
	r := mux.NewRouter()
	r.Use(CorrelationMiddleware)
	r.HandleFunc("/regular", regular_handle)

	//set global vars and listen on endpoint
	port = "4002"
	orch_token = "4fac636a-33f0-4f4a-9a19-c3ed5dddf75b"
	err := http.ListenAndServe(":"+port, r)
	log.Fatal(err)
}
