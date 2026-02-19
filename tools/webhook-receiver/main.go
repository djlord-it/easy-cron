package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type request struct {
	Timestamp string            `json:"timestamp"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
}

type stats struct {
	Count        int64     `json:"count"`
	LastRequests []request `json:"last_requests"`
	Since        string    `json:"since"`
}

var (
	mu           sync.Mutex
	count        int64
	lastRequests []request
	since        time.Time
	maxStored    = 50
)

func main() {
	since = time.Now().UTC()

	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	http.HandleFunc("/hook", hookHandler)
	http.HandleFunc("/stats", statsHandler)
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	http.HandleFunc("/reset", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		count = 0
		lastRequests = nil
		since = time.Now().UTC()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "reset")
	})

	log.Printf("webhook-receiver listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func hookHandler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	req := request{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   headers,
		Body:      string(body),
	}

	mu.Lock()
	count++
	lastRequests = append(lastRequests, req)
	if len(lastRequests) > maxStored {
		lastRequests = lastRequests[len(lastRequests)-maxStored:]
	}
	current := count
	mu.Unlock()

	log.Printf("hook received #%d: %s", current, string(body))
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"received":%d}`, current)
}

func statsHandler(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	s := stats{
		Count:        count,
		LastRequests: lastRequests,
		Since:        since.Format(time.RFC3339),
	}
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}
