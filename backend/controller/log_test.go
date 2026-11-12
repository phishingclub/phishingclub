package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// a session whose role holds no permissions must not change the log level,
// and the attempt must be in the audit log
func TestSetLevelDeniedWithoutPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	auditBuf := &bytes.Buffer{}
	auditLogger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(auditBuf),
		zapcore.InfoLevel,
	)).Sugar()
	atom := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	ctrl := &Log{
		Common: Common{
			Response: api.NewJSONResponseHandler(),
			Logger:   zap.NewNop().Sugar(),
		},
		OptionService: &service.Option{
			Common: service.Common{Logger: zap.NewNop().Sugar(), AuditLogger: auditLogger},
		},
		// the database is never reached on the denied path
		Database:   nil,
		LoggerAtom: &atom,
	}

	session := &model.Session{
		IP: "192.0.2.10",
		User: &model.User{
			ID:   nullable.NewNullableWithValue(uuid.New()),
			Role: &model.Role{Name: data.RoleCompanyUser},
		},
	}

	r := gin.New()
	r.POST("/log", func(g *gin.Context) {
		g.Set("session", session)
		ctrl.SetLevel(g)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/log", strings.NewReader(`{"level":"error","dbLevel":"silent"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if atom.Level() != zapcore.InfoLevel {
		t.Fatalf("log level changed to %s without permission", atom.Level())
	}
	got := auditBuf.String()
	if !strings.Contains(got, `"name":"Log.SetLevel"`) || !strings.Contains(got, `"authorized":false`) {
		t.Fatalf("denied attempt missing from audit log: %q", got)
	}
	if !strings.Contains(got, `"ip":"192.0.2.10"`) {
		t.Fatalf("audit event should carry the client IP: %q", got)
	}
}
