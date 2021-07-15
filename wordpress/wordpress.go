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

var port string
var orch_token string

//mongoDB constants, change them for your own DB
const (
	hosts      = "localhost:27017" //IP
	database   = "logs"            //DB (schema)
	username   = ""                //login details
	password   = ""                //login details
	collection = "wordpress"       //Collection (table)
)

type Log struct {
	Correlation_ID string
	Date           interface{}
	URL            string
	Status_code    int
	Check          string
}

type Orchestrator struct {
	URL      string
	Platform string //ignored but stored
	Check    []string
}

//structs to store and filter data
type PluginStatus struct {
	Name   string
	Status string
}
type UserStatus struct {
	Name  string
	Roles string
	Link  string
}
type ConfigStatus struct {
	Title               string
	URL                 string
	Email               string
	Default_ping_status string
}

type MongoStore struct {
	session *mgo.Session
}

var mongoStore = MongoStore{}

func wordpress_handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Orch-Token") == orch_token {
		if r.Header.Get("Correlation-ID") != "" {
			if r.Header.Get("Content-Type") != "application/json" {
				msg := "Content type should be application/json, not: " + r.Header.Get("Content-Type")
				http.Error(w, msg, http.StatusBadRequest)
			}
		} else {
			msg := "No correlation ID set"
			http.Error(w, msg, http.StatusBadRequest)
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

		//log information received
		logger.Infof("%v", orch)

		// Catch if there's multiple JSON objects
		err = dec.Decode(&struct{}{})
		if err != io.EOF {
			msg := "Request body must only contain a single JSON object"
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		//config = "https://"+orch.URL+"/wp-json/wp/v2/settings"
		//plugins = "https://"+orch.URL+"/wp-json/wp/v2/plugins"
		//users = "https://"+orch.URL+"/wp-json/wp/v2/users"
		toLog.URL = orch.URL
		toLog.Correlation_ID = r.Header.Get("Correlation-ID")
		for _, v := range orch.Check {

			//plugins check
			if v == "plugins" {
				//struct to translate into
				toPlug := make([]PluginStatus, 0)
				//generate request
				req, err := http.NewRequest("GET", "https://"+orch.URL+"/wp-json/wp/v2/plugins", nil)
				//report error
				if err != nil {
					logger.Infof("Request Creation: " + err.Error())
					http.Error(w, "Request Creation Failed.", http.StatusBadRequest)
					return
				}
				//add headers to request
				req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
				req.Header.Set("Cookie", r.Header.Get("Cookie"))
				resp, err := http.DefaultClient.Do(req)
				//log the result to DB
				toLog.Status_code = resp.StatusCode
				toLog.Check = "plugins"
				toLog.Date = time.Now()
				newErr := col.Insert(toLog)
				if newErr != nil {
					logger.Infof("Logging Failed: " + newErr.Error())
					http.Error(w, "Logging Failed.", http.StatusBadRequest)
					return
				}
				//send request and report error
				if err != nil {
					logger.Infof("Request Sent: " + err.Error())
					http.Error(w, "Request Sent Failed.", http.StatusBadRequest)
					return
				}
				defer resp.Body.Close()
				//get string
				b, err := io.ReadAll(resp.Body)
				//report error
				if err != nil {
					logger.Infof("Response Received: " + err.Error())
					http.Error(w, "Response Received Failed.", http.StatusBadRequest)
					return
				}
				//translate into struct and report error
				//an error will be thrown when the nonce or cookie is out of date or incorrect
				err = json.Unmarshal(b, &toPlug)
				if err != nil {
					logger.Infof("Encode Plugins Error: " + err.Error())
					w.Write(b) //incorrect cookie nonce
					http.Error(w, "X-WP-Nonce is incorrect or out of date, or Cookie is incorrect or out of date.", http.StatusBadRequest)
					return
				}
				//filter useless info out using struct
				filtered, err := json.Marshal(toPlug)
				//report error
				if err != nil {
					logger.Infof("Decode Plugins Error: " + err.Error())
					http.Error(w, "Decode Plugins Error.", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				//log it and respond
				w.Write(filtered)

				//config or site settings check
			} else if v == "config" {
				//generate request
				req, err := http.NewRequest("GET", "https://"+orch.URL+"/wp-json/wp/v2/settings", nil)
				//report error
				if err != nil {
					logger.Infof("Request Creation: " + err.Error())
					http.Error(w, "Request Creation Failed.", http.StatusBadRequest)
					return
				}
				//add headers
				req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
				req.Header.Set("Cookie", r.Header.Get("Cookie"))
				//send request and report error
				resp, err := http.DefaultClient.Do(req)
				//log the result to DB
				toLog.Status_code = resp.StatusCode
				toLog.Check = "config"
				toLog.Date = time.Now()
				newErr := col.Insert(toLog)
				if newErr != nil {
					logger.Infof("Logging Failed: " + newErr.Error())
					http.Error(w, "Logging Failed.", http.StatusBadRequest)
					return
				}
				if err != nil {
					logger.Infof("Request Sent: " + err.Error())
					http.Error(w, "Request Sent Failed.", http.StatusBadRequest)
					return
				}
				defer resp.Body.Close()
				//get string
				b, err := io.ReadAll(resp.Body)
				//report error
				if err != nil {
					logger.Infof("Response Received: " + err.Error())
					http.Error(w, "Response Received Failed.", http.StatusBadRequest)
					return
				}
				//log it and respond
				w.Write(b)

				//users check
			} else if v == "users" {
				//struct to translate into
				toPlug := make([]UserStatus, 0)
				//generate request
				req, err := http.NewRequest("GET", "https://"+orch.URL+"/wp-json/wp/v2/users", nil)
				//report error
				if err != nil {
					logger.Infof("Request Creation: " + err.Error())
					http.Error(w, "Request Creation Failed.", http.StatusBadRequest)
					return
				}
				//add headers
				req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
				req.Header.Set("Cookie", r.Header.Get("Cookie"))
				resp, err := http.DefaultClient.Do(req)
				//log the result to DB
				toLog.Status_code = resp.StatusCode
				toLog.Check = "users"
				toLog.Date = time.Now()
				newErr := col.Insert(toLog)
				if newErr != nil {
					logger.Infof("Logging Failed: " + newErr.Error())
					http.Error(w, "Logging Failed.", http.StatusBadRequest)
					return
				}
				//send request and report error
				if err != nil {
					logger.Infof("Request Sent: " + err.Error())
					http.Error(w, "Request Sent Failed.", http.StatusBadRequest)
					return
				}
				defer resp.Body.Close()
				//get string
				b, err := io.ReadAll(resp.Body)
				//report error
				if err != nil {
					logger.Infof("Response Received: " + err.Error())
					http.Error(w, "Response Received Failed.", http.StatusBadRequest)
					return
				}
				//translate into struct and report error
				//an error will be thrown when the nonce or cookie is out of date or incorrect
				err = json.Unmarshal(b, &toPlug)
				if err != nil {
					logger.Infof("Encode Users Error: " + err.Error())
					w.Write(b) //incorrect cookie nonce
					http.Error(w, "X-WP-Nonce is incorrect or out of date, or Cookie is incorrect or out of date.", http.StatusBadRequest)
					return
				}
				//filter useless info out using struct
				filtered, err := json.Marshal(toPlug)
				//report error
				if err != nil {
					logger.Infof("Decode Users Error: " + err.Error())
					http.Error(w, "Decode Users Error.", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				//log it and respond
				w.Write(filtered)

				//invalid check
			} else {
				logger.Infof("Incorrect Check: \"" + v + "\"")
				fmt.Fprintf(w, "Incorrect Check: \""+v+"\"")
			}
		}

		//basic check
		resp, err := http.Get("https://" + orch.URL)
		//log the result to DB
		toLog.Status_code = resp.StatusCode
		toLog.Check = "basic"
		toLog.Date = time.Now()
		newErr := col.Insert(toLog)
		if newErr != nil {
			logger.Infof("Logging Failed: " + newErr.Error())
			http.Error(w, "Logging Failed.", http.StatusBadRequest)
			return
		}
		if err != nil {
			logger.Infof("Basic Request Error: " + err.Error())
			http.Error(w, "Basic Request Error.", http.StatusBadRequest)
			return
		} else if resp.StatusCode == 200 {
			if err != nil {
				panic(err)
			}
			logger.Infof("Status OK")
			//this Error method sometimes panics with 'http: superfluous response.WriteHeader call from main.wordpress_handle'
			http.Error(w, orch.URL+": OK", http.StatusOK)
			return
		} else {
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
	r.HandleFunc("/wordpress", wordpress_handle)

	//set global vars and listen on endpoint
	port = "4001"
	orch_token = "4fac636a-33f0-4f4a-9a19-c3ed5dddf75b"
	err := http.ListenAndServe(":"+port, r)
	log.Fatal(err)
}
