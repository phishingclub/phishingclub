package log

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/phishingclub/phishingclub/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// buffer is a goroutine safe write syncer for tests
type buffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) Sync() error { return nil }

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func newMainCore(level zap.AtomicLevel, out zapcore.WriteSyncer) zapcore.Core {
	return zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		out,
		level,
	)
}

func TestAuditIgnoresMainLogLevel(t *testing.T) {
	for _, lvl := range []zapcore.Level{zapcore.WarnLevel, zapcore.ErrorLevel, zapcore.FatalLevel} {
		out := &buffer{}
		atom := zap.NewAtomicLevelAt(zapcore.InfoLevel)
		mainCore := newMainCore(atom, out)
		main := zap.New(mainCore).Sugar()
		audit := NewAuditLogger(mainCore, nil).Sugar()

		atom.SetLevel(lvl)
		main.Infow("normal info line")
		audit.Infow("audit", "name", "Option.SetOptionByKey")

		got := out.String()
		if strings.Contains(got, "normal info line") {
			t.Fatalf("level %s: main logger should drop info", lvl)
		}
		if !strings.Contains(got, `"name":"Option.SetOptionByKey"`) {
			t.Fatalf("level %s: audit event missing from output: %q", lvl, got)
		}
	}
}

func TestAuditDropsDebug(t *testing.T) {
	out := &buffer{}
	atom := zap.NewAtomicLevelAt(zapcore.DebugLevel)
	mainCore := newMainCore(atom, out)
	audit := NewAuditLogger(mainCore, nil).Sugar()
	audit.Debugw("debug line")
	if out.String() != "" {
		t.Fatalf("audit logger should not write debug, got %q", out.String())
	}
}

func TestAuditKeepsMainFields(t *testing.T) {
	out := &buffer{}
	atom := zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	mainCore := newMainCore(atom, out)
	audit := NewAuditLogger(mainCore, nil).Sugar().With("v-debug", "1.2.3")
	audit.Infow("audit", "name", "x")
	if !strings.Contains(out.String(), `"v-debug":"1.2.3"`) {
		t.Fatalf("fields added with With are missing: %q", out.String())
	}
}

func TestAuditFileGetsEventsAndMainOutputToo(t *testing.T) {
	out := &buffer{}
	file := &buffer{}
	atom := zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	mainCore := newMainCore(atom, out)
	audit := NewAuditLogger(mainCore, file).Sugar()

	audit.Infow("audit", "name", "User.Login", "userId", "u1")

	if !strings.Contains(out.String(), `"name":"User.Login"`) {
		t.Fatalf("audit event missing from main output: %q", out.String())
	}
	lines := strings.Split(strings.TrimSpace(file.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one line in audit file, got %d: %q", len(lines), file.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatalf("audit file line is not JSON: %v", err)
	}
	if m["msg"] != "audit" || m["name"] != "User.Login" || m["userId"] != "u1" {
		t.Fatalf("unexpected audit file line: %v", m)
	}
}

func TestOpenAuditFileModeAndAppend(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	if err := os.WriteFile(p, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenAuditFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("next\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	b, _ := os.ReadFile(p)
	if string(b) != "existing\nnext\n" {
		t.Fatalf("file should be appended, got %q", string(b))
	}

	newPath := filepath.Join(t.TempDir(), "new-audit.log")
	f, err = OpenAuditFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	st, err := os.Stat(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("new audit file must not be readable by group or others, got %v", st.Mode().Perm())
	}
}

func TestOpenAuditFileFailsOnMissingDir(t *testing.T) {
	_, err := OpenAuditFile(filepath.Join(t.TempDir(), "missing", "audit.log"))
	if err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

// uses the real production and development loggers from factory.go with a
// log file, sets the level to error and checks audit events still reach the
// log file and the audit file while normal info lines are dropped
func TestAuditWithFactoryLoggers(t *testing.T) {
	type factory func(*config.Config) (*zap.Logger, *zap.AtomicLevel, error)
	for name, newLogger := range map[string]factory{
		"production":  NewProductionLogger,
		"development": NewDevelopmentLogger,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			conf := config.NewDevDefaultConfig()
			conf.LogPath = filepath.Join(dir, "app.log")
			conf.AuditLogPath = filepath.Join(dir, "audit.log")

			mainLogger, atom, err := newLogger(conf)
			if err != nil {
				t.Fatal(err)
			}
			f, err := OpenAuditFile(conf.AuditLogPath)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			main := mainLogger.Sugar().With("v-debug", "test")
			audit := NewAuditLogger(mainLogger.Core(), f).Sugar().With("v-debug", "test")

			atom.SetLevel(zapcore.ErrorLevel)
			main.Infow("normal info line")
			audit.Infow("audit", "name", "Log.SetLevel", "authorized", true)
			_ = main.Sync()
			_ = audit.Sync()

			logFile, _ := os.ReadFile(conf.LogPath)
			if strings.Contains(string(logFile), "normal info line") {
				t.Fatal("info line should be dropped at error level")
			}
			if !strings.Contains(string(logFile), "Log.SetLevel") {
				t.Fatalf("audit event missing from log file: %q", string(logFile))
			}
			auditFile, _ := os.ReadFile(conf.AuditLogPath)
			if !strings.Contains(string(auditFile), `"name":"Log.SetLevel"`) ||
				!strings.Contains(string(auditFile), `"v-debug":"test"`) {
				t.Fatalf("audit file missing event or fields: %q", string(auditFile))
			}
		})
	}
}

func TestOpenAuditFileTightensExistingMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	if err := os.WriteFile(p, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenAuditFile(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != AuditFileMode {
		t.Fatalf("existing audit file mode should be %v, got %v", AuditFileMode, st.Mode().Perm())
	}
}

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.log")
	if err := os.Symlink(logPath, link); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, logPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", logPath, logPath, true},
		{"symlink", link, logPath, true},
		{"relative and absolute", rel, logPath, true},
		{"not cleaned", filepath.Join(dir, ".", "app.log"), logPath, true},
		{"file url", "file://" + logPath, logPath, true},
		{"missing audit file", filepath.Join(dir, "audit.log"), logPath, false},
		{"both missing same path", filepath.Join(dir, "x.log"), filepath.Join(dir, "x.log"), true},
		{"both missing other path", filepath.Join(dir, "x.log"), filepath.Join(dir, "y.log"), false},
	}
	for _, c := range cases {
		if got := SameFile(c.a, c.b); got != c.want {
			t.Errorf("%s: SameFile(%q, %q) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}
