package log

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/phishingclub/phishingclub/errs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AuditFileMode is the file mode of the audit log file.
// Audit events hold user ids, user names and client IPs, so only the owner
// may read the file.
const AuditFileMode os.FileMode = 0o600

// auditCore writes audit events through the main log core while ignoring the
// level the main logger is set to. The main log level can be changed at
// runtime from the admin UI, and that must never turn audit logging off.
// Writing through the main core keeps the output, format and order the same
// as normal log lines.
type auditCore struct {
	inner zapcore.Core
}

// NewAuditCore returns a core that always writes info and higher entries to
// inner, no matter which level inner is set to.
func NewAuditCore(inner zapcore.Core) zapcore.Core {
	return &auditCore{inner: inner}
}

func (c *auditCore) Enabled(lvl zapcore.Level) bool {
	return lvl >= zapcore.InfoLevel
}

func (c *auditCore) With(fields []zapcore.Field) zapcore.Core {
	return &auditCore{inner: c.inner.With(fields)}
}

func (c *auditCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Write skips the level check of inner on purpose, the level was already
// checked by Enabled.
func (c *auditCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	return c.inner.Write(ent, fields)
}

func (c *auditCore) Sync() error {
	return c.inner.Sync()
}

// OpenAuditFile opens or creates the audit log file for appending. The mode
// of a file that already exists is set to AuditFileMode as well.
func OpenAuditFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, AuditFileMode)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := f.Chmod(AuditFileMode); err != nil {
		_ = f.Close()
		return nil, errs.Wrap(err)
	}
	return f, nil
}

// SameFile reports if two log paths point to the same file. A file:// prefix
// as accepted by the main log path is removed first. Paths that do not exist
// yet are compared as absolute paths.
func SameFile(a string, b string) bool {
	a = strings.TrimPrefix(a, "file://")
	b = strings.TrimPrefix(b, "file://")
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ai, bi)
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return absA == absB
}

// NewAuditFileCore returns a core that writes info and higher entries as JSON
// lines to w.
func NewAuditFileCore(w zapcore.WriteSyncer) zapcore.Core {
	return zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.Lock(w),
		zapcore.InfoLevel,
	)
}

// NewAuditLogger builds the audit logger on top of the main log core. When
// file is not nil, audit events are also written to it.
func NewAuditLogger(mainCore zapcore.Core, file zapcore.WriteSyncer) *zap.Logger {
	core := NewAuditCore(mainCore)
	if file != nil {
		core = zapcore.NewTee(core, NewAuditFileCore(file))
	}
	return zap.New(core)
}
