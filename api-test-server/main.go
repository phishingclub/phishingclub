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
	mux.HandleFunc("GET /test-login", handleLoginPage)
	mux.HandleFunc("POST /test-login", handleLogin)
	mux.HandleFunc("GET /test-dashboard", handleDashboard)
	mux.HandleFunc("POST /test-logout", handleLogout)
	mux.HandleFunc("GET /test-json-api", handleJSONAPI)
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

// login test page handlers
func handleLoginPage(w http.ResponseWriter, req *http.Request) {
	log.Println("serving login test page")
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Login Test Page</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        .auth-method { margin: 20px 0; padding: 15px; border: 1px solid #ddd; border-radius: 5px; }
        .auth-method.active { border-color: #007bff; background-color: #f0f8ff; }
        input, button { margin: 5px 0; padding: 8px; }
        button { background-color: #007bff; color: white; border: none; cursor: pointer; border-radius: 3px; }
        button:hover { background-color: #0056b3; }
        .info { background-color: #e7f3ff; padding: 10px; border-radius: 5px; margin: 20px 0; }
        .status { margin: 20px 0; padding: 10px; border-radius: 5px; }
        .success { background-color: #d4edda; color: #155724; }
        .error { background-color: #f8d7da; color: #721c24; }
    </style>
</head>
<body>
    <h1>Login Test Page</h1>
    <div class="info">
        <strong>Credentials:</strong> admin / admin<br>
        <strong>Purpose:</strong> Test proxy capture engines
    </div>

    <div class="auth-method active" id="method-urlencoded">
        <h3>URL Encoded Form (application/x-www-form-urlencoded)</h3>
        <form id="form-urlencoded">
            <input type="text" name="username" placeholder="Username" value="admin" required><br>
            <input type="password" name="password" placeholder="Password" value="admin" required><br>
            <button type="submit">Login (URL Encoded)</button>
        </form>
    </div>

    <div class="auth-method" id="method-json">
        <h3>JSON (application/json)</h3>
        <form id="form-json">
            <input type="text" name="username" placeholder="Username" value="admin" required><br>
            <input type="password" name="password" placeholder="Password" value="admin" required><br>
            <button type="submit">Login (JSON)</button>
        </form>
    </div>

    <div class="auth-method" id="method-formdata">
        <h3>Form Data (multipart/form-data)</h3>
        <form id="form-formdata" enctype="multipart/form-data">
            <input type="text" name="username" placeholder="Username" value="admin" required><br>
            <input type="password" name="password" placeholder="Password" value="admin" required><br>
            <button type="submit">Login (Form Data)</button>
        </form>
    </div>

    <div id="status"></div>

    <script>
        function setStatus(message, isError) {
            const status = document.getElementById('status');
            status.textContent = message;
            status.className = 'status ' + (isError ? 'error' : 'success');
            setTimeout(() => status.textContent = '', 5000);
        }

        // url encoded form
        document.getElementById('form-urlencoded').addEventListener('submit', async (e) => {
            e.preventDefault();
            const formData = new FormData(e.target);
            const params = new URLSearchParams(formData);

            try {
                const response = await fetch('/test-login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                    body: params.toString()
                });
                const data = await response.json();
                if (response.ok) {
                    setStatus('Login successful (URL Encoded)! Cookie set. Redirecting...', false);
                    setTimeout(() => window.location.href = '/test-dashboard', 1500);
                } else {
                    setStatus('Login failed: ' + data.error, true);
                }
            } catch (err) {
                setStatus('Error: ' + err.message, true);
            }
        });

        // json form
        document.getElementById('form-json').addEventListener('submit', async (e) => {
            e.preventDefault();
            const formData = new FormData(e.target);
            const data = {
                username: formData.get('username'),
                password: formData.get('password')
            };

            try {
                const response = await fetch('/test-login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(data)
                });
                const result = await response.json();
                if (response.ok) {
                    setStatus('Login successful (JSON)! Cookie set. Redirecting...', false);
                    setTimeout(() => window.location.href = '/test-dashboard', 1500);
                } else {
                    setStatus('Login failed: ' + result.error, true);
                }
            } catch (err) {
                setStatus('Error: ' + err.message, true);
            }
        });

        // formdata form
        document.getElementById('form-formdata').addEventListener('submit', async (e) => {
            e.preventDefault();
            const formData = new FormData(e.target);

            try {
                const response = await fetch('/test-login', {
                    method: 'POST',
                    body: formData
                });
                const data = await response.json();
                if (response.ok) {
                    setStatus('Login successful (Form Data)! Cookie set. Redirecting...', false);
                    setTimeout(() => window.location.href = '/test-dashboard', 1500);
                } else {
                    setStatus('Login failed: ' + data.error, true);
                }
            } catch (err) {
                setStatus('Error: ' + err.message, true);
            }
        });
    </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

func handleLogin(w http.ResponseWriter, req *http.Request) {
	contentType := req.Header.Get("Content-Type")
	log.Printf("received login request with content-type: %s", contentType)

	var username, password string

	// parse based on content type
	if strings.Contains(contentType, "application/json") {
		var data map[string]string
		if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
			log.Println("failed to decode json:", err)
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		username = data["username"]
		password = data["password"]
		log.Printf("json login attempt: username=%s", username)
	} else if strings.Contains(contentType, "multipart/form-data") {
		if err := req.ParseMultipartForm(10 << 20); err != nil {
			log.Println("failed to parse multipart form:", err)
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form data"})
			return
		}
		username = req.FormValue("username")
		password = req.FormValue("password")
		log.Printf("formdata login attempt: username=%s", username)
	} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		if err := req.ParseForm(); err != nil {
			log.Println("failed to parse form:", err)
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form"})
			return
		}
		username = req.FormValue("username")
		password = req.FormValue("password")
		log.Printf("urlencoded login attempt: username=%s", username)
	} else {
		log.Printf("unsupported content type: %s", contentType)
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported content type"})
		return
	}

	// validate credentials
	if username == "admin" && password == "admin" {
		// set session cookie
		sessionID := fmt.Sprintf("session_%d", time.Now().Unix())
		cookie := &http.Cookie{
			Name:     "test_session",
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   3600,
		}
		http.SetCookie(w, cookie)
		log.Printf("login successful: username=%s, session=%s", username, sessionID)
		respondJSON(w, http.StatusOK, map[string]string{
			"message":    "login successful",
			"session_id": sessionID,
			"username":   username,
		})
	} else {
		log.Printf("login failed: invalid credentials for username=%s", username)
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
	}
}

func handleDashboard(w http.ResponseWriter, req *http.Request) {
	// check for session cookie
	cookie, err := req.Cookie("test_session")
	if err != nil {
		log.Println("no session cookie found, redirecting to login")
		http.Redirect(w, req, "/test-login", http.StatusSeeOther)
		return
	}

	log.Printf("dashboard access: session=%s", cookie.Value)
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Dashboard</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        .info { background-color: #d4edda; padding: 15px; border-radius: 5px; margin: 20px 0; }
        button { background-color: #dc3545; color: white; border: none; padding: 10px 20px; cursor: pointer; border-radius: 3px; margin: 5px; }
        button:hover { background-color: #c82333; }
        .session { background-color: #e7f3ff; padding: 10px; border-radius: 5px; margin: 20px 0; }
        .api-link { background-color: #28a745; }
        .api-link:hover { background-color: #218838; }
        .links { margin: 20px 0; }
    </style>
</head>
<body>
    <h1>Dashboard</h1>
    <div class="info">
        <strong>✓ Login Successful!</strong><br>
        You are now logged in.
    </div>
    <div class="session">
        <strong>Session ID:</strong> ` + cookie.Value + `
    </div>
    <div class="links">
        <button class="api-link" onclick="window.location.href='/test-json-api'">Test JSON API</button>
        <button onclick="logout()">Logout</button>
    </div>

    <script>
        async function logout() {
            try {
                const response = await fetch('/test-logout', { method: 'POST' });
                const data = await response.json();
                alert(data.message);
                window.location.href = '/test-login';
            } catch (err) {
                alert('Logout error: ' + err.message);
            }
        }
    </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

func handleLogout(w http.ResponseWriter, req *http.Request) {
	// get session cookie before clearing
	cookie, _ := req.Cookie("test_session")
	sessionID := ""
	if cookie != nil {
		sessionID = cookie.Value
	}

	// clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "test_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	log.Printf("logout successful: session=%s", sessionID)
	respondJSON(w, http.StatusOK, map[string]string{"message": "logout successful"})
}

func respondJSON(w http.ResponseWriter, status int, data map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func handleJSONAPI(w http.ResponseWriter, req *http.Request) {
	log.Println("serving json api test endpoint")

	data := map[string]interface{}{
		"secret": "1234",
		"config": map[string]interface{}{
			"url": "https://test.test",
		},
		"users": []map[string]interface{}{
			{
				"username": "foo",
				"password": "summervacation!!!!",
			},
			{
				"username": "alice",
				"password": "wonderland2024",
			},
			{
				"username": "bob",
				"password": "builder123",
			},
			{
				"username": "charlie",
				"password": "chocolate_factory",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
	log.Println("json api response sent")
}
