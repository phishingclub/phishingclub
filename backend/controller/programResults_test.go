package controller

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/api"
)

// TestProgramTimeRangeQuery checks the results take a time range key or none,
// and refuse anything else with 400
func TestProgramTimeRangeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		query string
		want  string
		ok    bool
	}{
		{"", "", true},
		{"?range=30d", "30d", true},
		{"?range=90d", "90d", true},
		{"?range=12m", "12m", true},
		{"?range=all", "all", true},
		{"?range=", "", false},
		{"?range=7d", "", false},
		{"?range=30d&range=90d", "", false},
		{"?range=%27%20OR%201%3D1", "", false},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(rec)
		g.Request = httptest.NewRequest(http.MethodGet, "/"+c.query, nil)
		got, ok := programTimeRangeQuery(g, api.NewJSONResponseHandler())
		if ok != c.ok || got != c.want {
			t.Fatalf("%q: got %q ok=%v", c.query, got, ok)
		}
		if !ok && rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: expected 400, got %d", c.query, rec.Code)
		}
	}
}

// TestWriteCSVStreamCut checks an export that fails before any byte is sent
// returns the error with the file headers cleared, and one that fails after
// bytes were sent breaks the download instead of ending it as a whole file
func TestWriteCSVStreamCut(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fail := errors.New("read failed")
	rows := func(n int) func(write func([]string) error) error {
		return func(write func([]string) error) error {
			for i := 0; i < n; i++ {
				if err := write([]string{strings.Repeat("x", 100)}); err != nil {
					return err
				}
			}
			return fail
		}
	}
	router := gin.New()
	// the same as middleware.AbortRecovery, which this package can not import
	router.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		if err == http.ErrAbortHandler {
			panic(err)
		}
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	router.GET("/:n", func(g *gin.Context) {
		n, _ := strconv.Atoi(g.Param("n"))
		g.Header("Content-Disposition", "attachment; filename=x.csv")
		g.Header("Content-Type", "text/csv")
		if err := writeCSVStream(g, rows(n)); err != nil {
			g.String(http.StatusInternalServerError, "failed")
		}
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	// nothing reached the client yet, so the error is answered
	res, err := http.Get(srv.URL + "/1")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusInternalServerError || string(body) != "failed" ||
		res.Header.Get("Content-Disposition") != "" {
		t.Fatalf("early error: %d %q %v %q", res.StatusCode, body, err, res.Header.Get("Content-Disposition"))
	}
	// many rows were sent, the body must not read as complete
	res, err = http.Get(srv.URL + "/1000")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil {
		t.Fatal("a cut export must not read as a complete file")
	}
}
