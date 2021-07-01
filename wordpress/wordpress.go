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

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

var port string

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
	Name string
	Link string
}
type ConfigStatus struct {
	Title               string
	URL                 string
	Email               string
	Default_ping_status string
}

func grabData(url string, w http.ResponseWriter, r *http.Request) []byte {
	logger := r.Context().Value("RequestLogger").(*logrus.Entry)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		logger.Infof(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	req.Header.Set("X-WP-Nonce", r.Header.Get("X-WP-Nonce"))
	req.Header.Set("Cookie", r.Header.Get("Cookie"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Infof(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	defer resp.Body.Close()
	//get string
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Infof(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	//return string, either ok or error
	return b
}

func wordpress_handle(w http.ResponseWriter, r *http.Request) {
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
	for _, v := range orch.Check {
		if v == "plugins" {
			toPlug := make([]PluginStatus, 0)
			data := grabData("https://"+orch.URL+"/wp-json/wp/v2/plugins", w, r)
			err := json.Unmarshal(data, &toPlug)
			if err != nil {
				logger.Infof("Encode Plugins Error: " + err.Error())
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			filtered, err := json.Marshal(toPlug)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				logger.Infof("Decode Plugins Error: " + err.Error())
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			w.Write(filtered)
		} else if v == "config" {
			data := grabData("https://"+orch.URL+"/wp-json/wp/v2/settings", w, r)
			w.Write(data)
		} else if v == "users" {
			toPlug := make([]UserStatus, 0)
			data := grabData("https://"+orch.URL+"/wp-json/wp/v2/users", w, r)
			err := json.Unmarshal(data, &toPlug)
			if err != nil {
				logger.Infof("Encode Users Error: " + err.Error())
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			filtered, err := json.Marshal(toPlug)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				logger.Infof("Decode Users Error: " + err.Error())
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			w.Write(filtered)
		} else {
			logger.Infof("Incorrect Check: \"" + v + "\"")
			fmt.Fprintf(w, "Incorrect Check: \""+v+"\"")
		}
	}
	logger.Infof("Status OK")
}

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

func main() {
	//mux := http.NewServeMux()
	r := mux.NewRouter()
	r.Use(CorrelationMiddleware)
	r.HandleFunc("/wordpress", wordpress_handle)

	port = "4001"
	err := http.ListenAndServe(":"+port, r)
	log.Fatal(err)
}
