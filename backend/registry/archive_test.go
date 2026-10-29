package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

type tarFile struct {
	name string
	body []byte
}

func buildTarGz(t *testing.T, files []tarFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		if err := tw.WriteHeader(
			&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body)), Typeflag: tar.TypeReg},
		); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func shaHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// buildPackage makes a valid archive holding the payload files plus a manifest
// that declares them, and returns it with its archive sha.
func buildPackage(t *testing.T, payload []tarFile, edit func(m *Manifest)) ([]byte, string) {
	t.Helper()
	m := Manifest{Format: "1", ID: "acme/login", Type: "page", Version: "1.0.0"}
	for _, f := range payload {
		m.Entries = append(m.Entries, Entry{Path: f.name, SHA256: shaHex(f.body)})
	}
	m.ContentHash = ComputeContentHash(m.Entries)
	if edit != nil {
		edit(&m)
	}
	mj, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	files := append([]tarFile{{name: ManifestFileName, body: mj}}, payload...)
	a := buildTarGz(t, files)
	return a, shaHex(a)
}

func TestVerifyAndExtractValid(t *testing.T) {
	a, sha := buildPackage(t, []tarFile{
		{name: "content/login/index.html", body: []byte("<p>hi</p>")},
		{name: "assets/a..b.png", body: []byte("png")},
	}, nil)
	pkg, err := VerifyAndExtract(a, strings.ToUpper(sha))
	if err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	if string(pkg.Files["assets/a..b.png"]) != "png" {
		t.Fatal("double dot file name not kept")
	}
}

func TestVerifyAndExtractUpperCaseHashes(t *testing.T) {
	a, sha := buildPackage(t, []tarFile{{name: "content/x/index.html", body: []byte("x")}}, func(m *Manifest) {
		for i := range m.Entries {
			m.Entries[i].SHA256 = strings.ToUpper(m.Entries[i].SHA256)
		}
		m.ContentHash = strings.ToUpper(ComputeContentHash(m.Entries))
	})
	if _, err := VerifyAndExtract(a, sha); err != nil {
		t.Fatalf("upper case hashes rejected: %v", err)
	}
}

func TestUntarRejectsParentSegment(t *testing.T) {
	for _, name := range []string{"../x", "content/../../x", "/etc/passwd", "content/..", "a\\b"} {
		a := buildTarGz(t, []tarFile{{name: name, body: []byte("x")}})
		if _, err := untar(a); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestUntarRejectsDuplicate(t *testing.T) {
	a := buildTarGz(t, []tarFile{{name: "content/a", body: []byte("1")}, {name: "content/a", body: []byte("2")}})
	if _, err := untar(a); err == nil {
		t.Fatal("duplicate entry accepted")
	}
}

func TestUntarEntryCount(t *testing.T) {
	files := make([]tarFile, 0, maxEntryCount+1)
	for i := 0; i <= maxEntryCount; i++ {
		files = append(files, tarFile{name: "content/f" + strconv.Itoa(i), body: nil})
	}
	if _, err := untar(buildTarGz(t, files)); err == nil || !strings.Contains(err.Error(), "too many entries") {
		t.Fatalf("expected entry count error, got %v", err)
	}
}

func TestUntarCompressionRatio(t *testing.T) {
	// zeros compress about 1000 to 1, so 20 MiB of them packs into a few KiB and
	// trips the ratio guard long before the absolute cap.
	a := buildTarGz(t, []tarFile{{name: "content/bomb", body: make([]byte, 20<<20)}})
	if int64(len(a))*maxCompressionRatio >= 20<<20 {
		t.Skip("archive compressed less than expected")
	}
	_, err := untar(a)
	if err == nil || !strings.Contains(err.Error(), "unpacks too large") {
		t.Fatalf("expected ratio error, got %v", err)
	}
}

func TestUntarSmallTextUnderFloor(t *testing.T) {
	// highly compressible but under the ratio floor, so it is accepted.
	a := buildTarGz(t, []tarFile{{name: "content/page.html", body: bytes.Repeat([]byte("<div></div>"), 100000)}})
	if _, err := untar(a); err != nil {
		t.Fatalf("small text archive rejected: %v", err)
	}
}

func TestUntarManifestCap(t *testing.T) {
	a := buildTarGz(t, []tarFile{{name: ManifestFileName, body: bytes.Repeat([]byte(" "), maxManifestBytes+1)}})
	if _, err := untar(a); err == nil {
		t.Fatal("oversized manifest accepted")
	}
}

func TestVerifyRejectsOversizedArchive(t *testing.T) {
	big := make([]byte, maxArchiveBytes+1)
	if _, err := VerifyAndExtract(big, shaHex(big)); err == nil {
		t.Fatal("oversized archive accepted")
	}
}

// TestVerifyErrorsArePlain: every archive check fails with a short "the
// package archive" or "the package manifest" message and no parser text.
func TestVerifyErrorsArePlain(t *testing.T) {
	big := make([]byte, maxArchiveBytes+1)
	noManifest := buildTarGz(t, []tarFile{{name: "content/a.html", body: []byte("x")}})
	badManifest := buildTarGz(t, []tarFile{{name: ManifestFileName, body: []byte("{not json")}})
	for _, c := range []struct {
		name    string
		archive []byte
		sha     string
		want    string
	}{
		{"empty", nil, "x", "the package archive is empty"},
		{"too large", big, shaHex(big), "the package archive is too large"},
		{"no sha", noManifest, "", "the package archive has no sha256 from the Bait Box"},
		{"sha mismatch", noManifest, "x", "the package archive does not match its sha256"},
		{"no manifest", noManifest, shaHex(noManifest), "the package archive has no " + ManifestFileName},
		{"bad manifest", badManifest, shaHex(badManifest), "the package manifest could not be read"},
	} {
		_, err := VerifyAndExtract(c.archive, c.sha)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
	_, err := VerifyAndExtract(badManifest, shaHex(badManifest))
	if !errors.Is(err, ErrBadManifest) {
		t.Fatalf("bad manifest: want ErrBadManifest, got %v", err)
	}
}

func TestManifestValidate(t *testing.T) {
	good := func() *Manifest {
		e := []Entry{{Path: "content/a.html", SHA256: strings.Repeat("a", 64)}}
		return &Manifest{ID: "acme/x", Type: "page", Version: "1.2.3", Entries: e, ContentHash: ComputeContentHash(e)}
	}
	if err := good().Validate(); err != nil {
		t.Fatalf("good manifest rejected: %v", err)
	}
	def := "Prod"
	cases := map[string]func(m *Manifest){
		"type":     func(m *Manifest) { m.Type = "evil" },
		"version":  func(m *Manifest) { m.Version = "latest" },
		"abs path": func(m *Manifest) { m.Entries[0].Path = "/content/a.html" },
		"dotdot":   func(m *Manifest) { m.Entries[0].Path = "content/../a.html" },
		"unclean":  func(m *Manifest) { m.Entries[0].Path = "content//a.html" },
		"dot":      func(m *Manifest) { m.Entries[0].Path = "./content/a.html" },
		"dup":      func(m *Manifest) { m.Entries = append(m.Entries, m.Entries[0]) },
		"sha":      func(m *Manifest) { m.Entries[0].SHA256 = "zz" },
		"manifest": func(m *Manifest) { m.Entries[0].Path = ManifestFileName },
		"dup variable": func(m *Manifest) {
			m.Variables = []Variable{{Name: "PROV", Type: "string"}, {Name: "PROV", Type: "string"}}
		},
		"dup pick variable": func(m *Manifest) {
			m.Variables = []Variable{
				{Name: "PROV", Type: "string", Pick: "oauthProvider"},
				{Name: "PROV", Type: "string", Default: &def},
			}
		},
		"pick default": func(m *Manifest) {
			m.Variables = []Variable{{Name: "PROV", Type: "string", Pick: "oauthProvider", Default: &def}}
		},
		"deps": func(m *Manifest) {
			for i := 0; i <= maxDependencies; i++ {
				m.Dependencies = append(m.Dependencies, Dependency{ID: "acme/d" + strconv.Itoa(i), Range: "*"})
			}
		},
	}
	for name, edit := range cases {
		m := good()
		edit(m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	m := good()
	m.Entries[0].Path = "assets/a..b.png"
	if err := m.Validate(); err != nil {
		t.Errorf("double dot inside a segment rejected: %v", err)
	}
	m = good()
	m.Variables = []Variable{
		{Name: "PROV", Type: "string", Pick: "oauthProvider", Required: true},
		{Name: "SUBJECT", Type: "string", Default: &def},
	}
	if err := m.Validate(); err != nil {
		t.Errorf("distinct variables rejected: %v", err)
	}
}

func TestManifestVariableNames(t *testing.T) {
	good := func(name string) *Manifest {
		e := []Entry{{Path: "content/a.html", SHA256: strings.Repeat("a", 64)}}
		return &Manifest{
			ID: "acme/x", Type: "page", Version: "1.2.3", Entries: e, ContentHash: ComputeContentHash(e),
			Variables: []Variable{{Name: name, Type: "string"}},
		}
	}
	for _, name := range []string{"A", "_", "PROV", "SENDER_NAME_2", "_X", strings.Repeat("A", 64)} {
		if err := good(name).Validate(); err != nil {
			t.Errorf("%q rejected: %v", name, err)
		}
		if !ValidVariableName(name) {
			t.Errorf("ValidVariableName(%q) = false", name)
		}
	}
	for _, name := range []string{
		"", "prov", "Prov", "1PROV", "PRO-V", "PRO V", "PRO.V", "@@PROV@@", "PRO@@V",
		"ns/pick\x1fPROVIDER", "PROV\x1f", "PROV\n", "PROV\x00", "ÆBLE", strings.Repeat("A", 65),
	} {
		err := good(name).Validate()
		want := fmt.Sprintf("it declares the variable name %q, which is not valid", name)
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", name, err, want)
		}
		if ValidVariableName(name) {
			t.Errorf("ValidVariableName(%q) = true", name)
		}
	}
}

func TestManifestVariableErrorsArePlain(t *testing.T) {
	def := "Prod"
	e := []Entry{{Path: "content/a.html", SHA256: strings.Repeat("a", 64)}}
	m := &Manifest{ID: "acme/x", Type: "page", Version: "1.2.3", Entries: e, ContentHash: ComputeContentHash(e)}
	m.Variables = []Variable{{Name: "PROV", Type: "string"}, {Name: "PROV", Type: "string"}}
	if err := m.Validate(); err == nil || err.Error() != `it declares the variable "PROV" more than once` {
		t.Errorf("duplicate: got %v", err)
	}
	m.Variables = []Variable{{Name: "PROV", Type: "string", Pick: "oauthProvider", Default: &def}}
	if err := m.Validate(); err == nil || err.Error() != `it gives the pick variable "PROV" a default value` {
		t.Errorf("pick default: got %v", err)
	}
}

func TestUntarSkippedEntryBomb(t *testing.T) {
	// an entry type the reader skips still has its data unpacked by the tar reader,
	// so a gigabyte of zeros in one must hit the stream cap, not be read through.
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	tw := tar.NewWriter(gz)
	size := int64(1 << 30)
	hdr := &tar.Header{Name: "content/x", Mode: 0o644, Size: size, Typeflag: tar.TypeCont}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	zero := make([]byte, 1<<20)
	for i := int64(0); i < size>>20; i++ {
		if _, err := tw.Write(zero); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	if _, err := untar(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "unpacks too large") {
		t.Fatalf("expected stream cap error, got %v", err)
	}
}

func TestUntarManySmallBombs(t *testing.T) {
	// many entries each under the per file cap must still stop at the total budget.
	files := make([]tarFile, 0, 40)
	for i := 0; i < 40; i++ {
		files = append(files, tarFile{name: "content/f" + strconv.Itoa(i), body: make([]byte, 4<<20)})
	}
	if _, err := untar(buildTarGz(t, files)); err == nil || !strings.Contains(err.Error(), "unpacks too large") {
		t.Fatalf("expected budget error, got %v", err)
	}
}

func TestUntarShortBody(t *testing.T) {
	// a header that claims more bytes than the stream holds is a read error.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	var hdr bytes.Buffer
	tw := tar.NewWriter(&hdr)
	th := &tar.Header{Name: "content/a", Mode: 0o644, Size: 1 << 40, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(th); err != nil {
		t.Fatal(err)
	}
	_ = tw.Flush()
	_, _ = gz.Write(hdr.Bytes()[:512])
	_, _ = gz.Write([]byte("short"))
	_ = gz.Close()
	if _, err := untar(buf.Bytes()); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("short body: want ErrBadArchive, got %v", err)
	}
}

func TestUntarCorruptStreamIsPlain(t *testing.T) {
	// not gzip at all, and gzip holding bytes that are not a tar stream.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(bytes.Repeat([]byte{0xff}, 1024))
	_ = gz.Close()
	for name, in := range map[string][]byte{"not gzip": []byte("plain text"), "not tar": buf.Bytes()} {
		_, err := untar(in)
		if !errors.Is(err, ErrBadArchive) || err.Error() != "the package archive could not be read" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestUntarPAXPathEscape(t *testing.T) {
	// a long name goes into a PAX record, and the record value is what is checked.
	name := "content/" + strings.Repeat("a", 120) + "/../../../x"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(
		&tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg, Format: tar.FormatPAX},
	); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = gz.Close()
	if _, err := untar(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

// pseudoRandom returns n bytes that gzip cannot compress.
func pseudoRandom(n int) []byte {
	rnd := make([]byte, n)
	h := sha256.Sum256([]byte("seed"))
	for i := 0; i < n; i += 32 {
		h = sha256.Sum256(h[:])
		copy(rnd[i:], h[:])
	}
	return rnd
}

func TestUntarFullSizeIncompressible(t *testing.T) {
	// a near full size archive (15 MiB of random bytes in one file plus many small
	// text files) stays under every cap.
	files := []tarFile{{name: "attachments/big.bin", body: pseudoRandom(15 << 20)}}
	for i := 0; i < maxEntryCount-100; i++ {
		files = append(files, tarFile{name: "content/p" + strconv.Itoa(i) + "/index.html", body: []byte("<p>page</p>")})
	}
	a := buildTarGz(t, files)
	if int64(len(a)) > maxArchiveBytes {
		t.Fatalf("test archive is %d bytes, over the archive cap", len(a))
	}
	if _, err := VerifyAndExtract(a, "x"); err == nil || strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected only the sha check to fail, got %v", err)
	}
	if _, err := untar(a); err != nil {
		t.Fatalf("legit archive rejected: %v", err)
	}
}

func TestUntarEntryOverFileCap(t *testing.T) {
	// one incompressible file just over the per file cap is refused by name.
	a := buildTarGz(t, []tarFile{{name: "attachments/big.bin", body: pseudoRandom(maxEntryBytes + 1)}})
	if _, err := untar(a); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected per file cap error, got %v", err)
	}
}

func TestUntarTotalCap(t *testing.T) {
	// three incompressible files each under the per file cap pass the total cap.
	files := []tarFile{
		{name: "attachments/a.bin", body: pseudoRandom(12 << 20)},
		{name: "attachments/b.bin", body: pseudoRandom(12 << 20)},
		{name: "attachments/c.bin", body: pseudoRandom(12 << 20)},
	}
	if _, err := untar(buildTarGz(t, files)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected total cap error, got %v", err)
	}
}

func TestUntarDirectoriesDoNotCountAsFiles(t *testing.T) {
	// Bait Box accepts up to maxEntryCount files plus the content and assets
	// directory entries, so the consumer must accept the same archive.
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	for _, d := range []string{"content/", "assets/"} {
		if err := tw.WriteHeader(&tar.Header{Name: d, Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < maxEntryCount; i++ {
		body := []byte("x")
		if err := tw.WriteHeader(
			&tar.Header{
				Name: "content/f" + strconv.Itoa(i), Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
			},
		); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := untar(buf.Bytes()); err != nil {
		t.Fatalf("archive at the file cap with directory entries rejected: %v", err)
	}
}

func TestUntarCompressibleUnderBaitBoxFloor(t *testing.T) {
	// Bait Box only checks the ratio past 10 MiB unpacked, so 9 MiB of very
	// compressible text must still unpack here.
	a := buildTarGz(t, []tarFile{{name: "content/page.html", body: bytes.Repeat([]byte("<div></div>"), (9<<20)/11)}})
	if _, err := untar(a); err != nil {
		t.Fatalf("compressible archive under the floor rejected: %v", err)
	}
}
