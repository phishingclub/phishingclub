package repository

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newOAuthStateRepo(t *testing.T) *OAuthState {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "oauthstate.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.OAuthState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &OAuthState{DB: db}
}

func insertOAuthState(t *testing.T, r *OAuthState, token string, providerID uuid.UUID) uuid.UUID {
	t.Helper()
	expires := time.Now().Add(10 * time.Minute)
	id, err := r.Insert(context.Background(), &model.OAuthState{
		StateToken:      nullable.NewNullableWithValue(*vo.NewString255Must(token)),
		OAuthProviderID: nullable.NewNullableWithValue(providerID),
		ExpiresAt:       &expires,
	})
	if err != nil {
		t.Fatalf("insert state: %v", err)
	}
	return *id
}

// TestOAuthStateMarkAsUsedOnce checks a state can be claimed only once
func TestOAuthStateMarkAsUsedOnce(t *testing.T) {
	r := newOAuthStateRepo(t)
	ctx := context.Background()
	id := insertOAuthState(t, r, "state-once", uuid.New())

	if err := r.MarkAsUsed(ctx, id); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := r.MarkAsUsed(ctx, id); !errors.Is(err, ErrOAuthStateAlreadyUsed) {
		t.Fatalf("second claim err = %v, want ErrOAuthStateAlreadyUsed", err)
	}
	if err := r.MarkAsUsed(ctx, uuid.New()); !errors.Is(err, ErrOAuthStateAlreadyUsed) {
		t.Fatalf("unknown id err = %v, want ErrOAuthStateAlreadyUsed", err)
	}
	got, err := r.GetByStateToken(ctx, "state-once")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Used || got.UsedAt == nil {
		t.Fatalf("state not marked used: %+v", got)
	}
}

// TestOAuthStateMarkAsUsedConcurrent checks that of many concurrent claims of
// the same state exactly one wins
func TestOAuthStateMarkAsUsedConcurrent(t *testing.T) {
	r := newOAuthStateRepo(t)
	ctx := context.Background()
	id := insertOAuthState(t, r, "state-race", uuid.New())

	const n = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.MarkAsUsed(ctx, id)
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
				return
			}
			if !errors.Is(err, ErrOAuthStateAlreadyUsed) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins = %d, want 1", wins)
	}
}

// TestOAuthStateFailedAndResult checks a provider error marks the state used
// and failed, and that the result is recorded for a claimed state
func TestOAuthStateFailedAndResult(t *testing.T) {
	r := newOAuthStateRepo(t)
	ctx := context.Background()
	providerID := uuid.New()
	insertOAuthState(t, r, "state-fail", providerID)

	got, err := r.MarkFailedByStateToken(ctx, "state-fail")
	if err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if got != providerID {
		t.Fatalf("provider id = %v, want %v", got, providerID)
	}
	if _, err := r.MarkFailedByStateToken(ctx, "state-fail"); !errors.Is(err, ErrOAuthStateAlreadyUsed) {
		t.Fatalf("second mark failed err = %v, want ErrOAuthStateAlreadyUsed", err)
	}
	if _, err := r.MarkFailedByStateToken(ctx, "unknown"); !errors.Is(err, ErrOAuthStateAlreadyUsed) {
		t.Fatalf("unknown token err = %v, want ErrOAuthStateAlreadyUsed", err)
	}
	st, err := r.GetByStateToken(ctx, "state-fail")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !st.Used || st.Result != database.OAUTH_STATE_RESULT_FAILED {
		t.Fatalf("state = used %v result %q, want used and failed", st.Used, st.Result)
	}
	// a failed state can not be claimed for a token exchange
	if err := r.MarkAsUsed(ctx, st.ID.MustGet()); !errors.Is(err, ErrOAuthStateAlreadyUsed) {
		t.Fatalf("claim after fail err = %v, want ErrOAuthStateAlreadyUsed", err)
	}

	id := insertOAuthState(t, r, "state-ok", providerID)
	if err := r.MarkAsUsed(ctx, id); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := r.SetResult(ctx, id, database.OAUTH_STATE_RESULT_SUCCESS); err != nil {
		t.Fatalf("set result: %v", err)
	}
	st, err = r.GetByStateToken(ctx, "state-ok")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if st.Result != database.OAUTH_STATE_RESULT_SUCCESS {
		t.Fatalf("result = %q, want success", st.Result)
	}
}
