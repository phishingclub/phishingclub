package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	To      string `json:"to"`
	From    string `json:"from"`
	Content string `json:"content"`
	APIKey  string `json:"apiKey"`
}

func (m *Message) isValid() error {
	if m.To == "" {
		return errors.New("missing 'to' field")
	}
	if m.From == "" {
		return errors.New("missing 'from' field")
	}
	if m.Content == "" {
		return errors.New("missing 'content' field")
	}
	if m.APIKey == "" {
		return errors.New("missing 'apiKey' field")
	}
	return nil
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api-sender/{clientID}", handleAPISender)
	mux.HandleFunc("POST /webhook", handleTestWebhook) // todo rename method and usage to test prefoxhl
	err := http.ListenAndServe(":80", mux)
	if err != nil {
		panic(err)
	}
}

func handleAPISender(w http.ResponseWriter, req *http.Request) {
	body1, body2, err := cloneBody(req)
	if err != nil {
		log.Println("failed to clone request body:", err)
		http.Error(w, "failed to clone request body", http.StatusInternalServerError)
		return
	}
	log.Println("received api send request")
	log.Println(prettyRequest(req, body1))

	clientID := req.PathValue("clientID")
	if clientID != "5200" {
		log.Println("invalid client ID")
		http.Error(w, "invalid client ID", http.StatusForbidden)
		return
	}
	// parse message
	msg := &Message{}
	dec := json.NewDecoder(body2)
	if err := dec.Decode(&msg); err != nil {
		log.Println("failed to decode message:", err)
		http.Error(w, "invalid message", http.StatusBadRequest)
		return
	}
	if err := msg.isValid(); err != nil {
		log.Println("invalid message:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sleepTime := time.Duration(rand.Intn(2)+1) * time.Second
	log.Printf("sleeping for %f seconds\n", sleepTime.Seconds())
	time.Sleep(sleepTime)

	// return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"data": "message sent"})
	log.Println("message sent successfully")
}

func handleTestWebhook(w http.ResponseWriter, req *http.Request) {
	log.Println("received webhook")
	body1, body2, err := cloneBody(req)
	if err != nil {
		log.Println("failed to clone request body:", err)
		http.Error(w, "failed to clone request body", http.StatusInternalServerError)
		return
	}
	log.Println(prettyRequest(req, body1))
	// sleep random time between 1 and 3 seconds
	time.Sleep(time.Duration(rand.Intn(2)+1) * time.Second)
	bodyBytes, err := io.ReadAll(body2)
	if err != nil {
		log.Println("failed to read body for HMAC calculation:", err)
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}
	// Calculate HMAC256
	// from seed/webhooks.go
	h := hmac.New(sha256.New, []byte("WEBHOOK_TEST_KEY@1234"))
	h.Write(bodyBytes)
	calculatedHMAC := hex.EncodeToString(h.Sum(nil))

	// Get the signature from the header
	signature := req.Header.Get("x-signature")
	if signature == "" {
		log.Println("missing x-signature header")
		http.Error(w, "missing x-signature header", http.StatusBadRequest)
		return
	}
	if signature != "UNSIGNED" {
		// Compare the calculated HMAC with the signature
		if calculatedHMAC != signature {
			log.Println("invalid HMAC signature")
			http.Error(w, "invalid HMAC signature", http.StatusForbidden)
			return
		}
		log.Println("valid HMAC signature")
	} else {
		log.Println("skipping HMAC signature")
	}

	// return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"data": "webhook processed"})
	//log.Printf("respone: %++v\n", w)
	log.Println("test webhook processed successfully")
}

func prettyRequest(req *http.Request, body io.ReadCloser) string {
	l := fmt.Sprintf("Request:\n\tMethod: %s\n\tURL: %s\n\tHeaders:\n", req.Method, req.URL)
	for k, v := range req.Header {
		// which headers have multiple values?
		value := strings.Join(v, "")
		l += fmt.Sprintf("\t\t%s: %s\n", k, value)
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return l + fmt.Sprintf("\tBody: failed to read body: %v\n", err)
	}
	l += fmt.Sprintf("\tBody: %s\n", string(b))
	return l
}

func cloneBody(req *http.Request) (io.ReadCloser, io.ReadCloser, error) {
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, nil, err
	}
	body1 := io.NopCloser(strings.NewReader(string(bodyBytes)))
	body2 := io.NopCloser(strings.NewReader(string(bodyBytes)))
	return body1, body2, nil
}
