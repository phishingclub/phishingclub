package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/model"
)

func companyIDQueryContext(rawQuery string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(rec)
	g.Request = httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	return g, rec
}

func TestStrictCompanyIDQuery(t *testing.T) {
	g, _ := companyIDQueryContext("")
	cid, ok := strictCompanyIDQuery(g, api.NewJSONResponseHandler())
	if !ok || cid != nil {
		t.Fatalf("no companyID must mean no company, got ok=%v cid=%v", ok, cid)
	}

	g, _ = companyIDQueryContext("companyID=8a7f3c2e-1b4d-4c6a-9e0f-123456789abc")
	cid, ok = strictCompanyIDQuery(g, api.NewJSONResponseHandler())
	if !ok || cid == nil || cid.String() != "8a7f3c2e-1b4d-4c6a-9e0f-123456789abc" {
		t.Fatalf("a valid companyID must parse, got ok=%v cid=%v", ok, cid)
	}

	g, rec := companyIDQueryContext("companyID=not-a-uuid")
	cid, ok = strictCompanyIDQuery(g, api.NewJSONResponseHandler())
	if ok || cid != nil {
		t.Fatalf("a bad companyID must be refused, got ok=%v cid=%v", ok, cid)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a bad companyID, got %d", rec.Code)
	}
}

// a bad companyID on the repeat offender count must be a 400, never all companies
func TestRepeatOffenderCountRefusesBadCompanyID(t *testing.T) {
	g, rec := companyIDQueryContext("companyID=nope")
	SetSessionInGinContext(g, &model.Session{User: &model.User{}})
	// the service is nil, so reaching it would panic: the 400 must come first
	r := &Recipient{Common: Common{Response: api.NewJSONResponseHandler()}}
	r.GetRepeatOffenderCount(g)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a bad companyID, got %d", rec.Code)
	}
}
