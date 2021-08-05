package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
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
	Plugin       string
	Name         string
	Version      string
	Status       string
	Requires_wp  string
	Requires_php string
	Plugin_uri   string
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

//new struct for sending, edits version, for two different types
type PluginSent struct {
	Name            string
	Version_current string
	Version_latest  string
	Status          string
	Requires_wp     string
	Requires_php    string
	Plugin_uri      string
}

type MongoStore struct {
	session *mgo.Session
}

var mongoStore = MongoStore{}

func log_update(logVar Log, logID, logURL string, logCheck string, logStatus_code int, logCol *mgo.Collection, logLogger *logrus.Entry) {
	logVar.Correlation_ID = logID
	logVar.URL = logURL
	logVar.Check = logCheck
	logVar.Status_code = logStatus_code
	logVar.Date = time.Now()
	err := logCol.Insert(logVar)
	if err != nil {
		panic(err)
	}
	logLogger.Infof("%v", logVar)
}

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
		userCheck := false
		pluginCheck := false
		configCheck := false
		for _, v := range orch.Check {
			//trim whitespace and any non-alphabetic character from it
			//since none of our checks use anything but alphabetic characters, this helps with typos involving other characters
			v = strings.TrimSpace(v)
			reg, err := regexp.Compile("[^a-zA-Z]+")
			if err != nil {
				http.Error(w, "Server Error!", http.StatusBadRequest)
				log.Fatal(err)
				return
			}
			v = reg.ReplaceAllString(v, "")
			//check and remove "s" from the end of each check
			runes := []rune(v)
			if string(runes[len(runes)-1]) == "s" {
				v = string(runes[:len(runes)-1])
			}
			if strings.ToLower(v) == "plugin" {
				pluginCheck = true
			} else if strings.ToLower(v) == "config" {
				configCheck = true
			} else if strings.ToLower(v) == "user" {
				userCheck = true
			} else {
				//invalid check
				logger.Infof("Incorrect Check: \"" + v + "\"")
				fmt.Fprintf(w, "Incorrect Check: \""+v+"\"")
			}
		}

		//plugins check
		if pluginCheck {
			//struct to translate into
			toPlug := make([]PluginStatus, 0)
			//generate request
			req, err := http.NewRequest("GET", "https://"+orch.URL+"/wp-json/wp/v2/plugins", nil)
			//report error
			if err != nil {
				logger.Infof("Request Creation: " + err.Error())
				log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, "plugins", 400, col, logger)
				http.Error(w, "Request Creation Failed.", http.StatusBadRequest)
				return
			}
			//add headers to request
			req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
			req.Header.Set("Cookie", r.Header.Get("Cookie"))
			resp, err := http.DefaultClient.Do(req)
			//log the result to DB
			log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, "plugins", resp.StatusCode, col, logger)
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
				http.Error(w, "Failed to fetch current version.", http.StatusBadRequest)
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

			//add code for checking versions, edit each toPlug and change version from string to boolean
			newPlug := make([]PluginSent, 0)
			for _, vPlug := range toPlug {
				latVersion := "N/A"
				split := strings.Split(vPlug.Plugin, "/")
				newResp, err := http.Get("https://api.wordpress.org/plugins/info/1.0/" + split[0])
				if err != nil {
					logger.Infof("New Response Received: " + err.Error())
					http.Error(w, "Failed to fetch latest version.", http.StatusBadRequest)
					return
				}
				defer newResp.Body.Close()
				responseData, err := ioutil.ReadAll(newResp.Body)
				if err != nil {
					http.Error(w, "Failed to fetch latest version.", http.StatusBadRequest)
					log.Fatal(err)
					return
				}
				stringData := []rune(string(responseData))
				if string(stringData[15]) == "2" && string(stringData[16]) == "5" {
					reg, err := regexp.Compile(";s:5:")
					if err != nil {
						http.Error(w, "Server Error!", http.StatusBadRequest)
						log.Fatal(err)
						return
					}
					location := reg.FindStringIndex(string(stringData))
					if location != nil {
						countChars := 0
						for _, vChar := range stringData[location[0]+6:] {
							if string(vChar) != "\"" {
								countChars++
							} else {
								break
							}
						}
						latVersion = string(stringData[location[0]+6 : location[0]+6+countChars])
					}
				}
				newPlug = append(newPlug, PluginSent{
					Name:            vPlug.Name,
					Version_current: vPlug.Version,
					Version_latest:  latVersion,
					Status:          vPlug.Status,
					Requires_wp:     vPlug.Requires_wp,
					Requires_php:    vPlug.Requires_php,
					Plugin_uri:      vPlug.Plugin_uri,
				})
			}

			//filter useless info out using struct
			filtered, err := json.Marshal(newPlug)
			//report error
			if err != nil {
				logger.Infof("Decode Plugins Error: " + err.Error())
				http.Error(w, "Decode Plugins Error.", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			//log it and respond
			w.Write(filtered)
		}

		//config or site settings check
		if configCheck {
			//generate request
			req, err := http.NewRequest("GET", "https://"+orch.URL+"/wp-json/wp/v2/settings", nil)
			//report error
			if err != nil {
				logger.Infof("Request Creation: " + err.Error())
				log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, "config", 400, col, logger)
				http.Error(w, "Request Creation Failed.", http.StatusBadRequest)
				return
			}
			//add headers
			req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
			req.Header.Set("Cookie", r.Header.Get("Cookie"))
			//send request and report error
			resp, err := http.DefaultClient.Do(req)
			//log the result to DB
			log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, "config", resp.StatusCode, col, logger)
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
		}

		//users check
		if userCheck {
			//struct to translate into
			toPlug := make([]UserStatus, 0)
			//generate request
			req, err := http.NewRequest("GET", "https://"+orch.URL+"/wp-json/wp/v2/users", nil)
			//report error
			if err != nil {
				logger.Infof("Request Creation: " + err.Error())
				log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, "users", 400, col, logger)
				http.Error(w, "Request Creation Failed.", http.StatusBadRequest)
				return
			}
			//add headers
			req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
			req.Header.Set("Cookie", r.Header.Get("Cookie"))
			resp, err := http.DefaultClient.Do(req)
			//log the result to DB
			log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, "users", resp.StatusCode, col, logger)
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
		}

		//basic check
		resp, err := http.Get("https://" + orch.URL)
		//log the result to DB
		log_update(toLog, r.Header.Get("Correlation-ID"), orch.URL, "basic", resp.StatusCode, col, logger)
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
