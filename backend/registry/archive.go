package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/phishingclub/phishingclub/safepath"
)

// Archive guards. The consumer unpacks a whole package into memory, so these
// bound what a hostile or corrupt download can make it allocate.
//
// Package payloads are mostly text plus images and attachments that are already
// compressed, so a legit archive unpacks to about its own size or a few times
// more for text. maxTotalBytes leaves room for a full size archive of
// incompressible files plus text. The ratio guard stops a small archive that
// expands far beyond what text compresses to (gzip of html is about 5 to 10
// times), once more than ratioFloorBytes is out. A registry may accept larger
// uploads than this; the consumer refuses an archive past these limits.
const (
	maxArchiveBytes     = 16 << 20 // 16 MiB compressed
	maxEntryBytes       = 16 << 20 // 16 MiB per file
	maxTotalBytes       = 32 << 20 // 32 MiB unpacked across the archive
	maxEntryCount       = 2000     // regular files
	maxOtherEntries     = 64       // directory and other entries, which carry no bytes
	maxCompressionRatio = 100      // unpacked bytes per compressed byte
	ratioFloorBytes     = 10 << 20 // ratio is enforced past this many unpacked bytes
	maxManifestBytes    = 1 << 20  // baitbox.json is small metadata
	// streamSlackBytes is what the tar stream may hold beyond the file bytes:
	// headers, PAX records and block padding, about 4 KiB per entry at most.
	streamSlackBytes = (maxEntryCount + maxOtherEntries) * (4 << 10)
)

// ErrBadArchive is returned when the gzip or tar stream is corrupt. The reader
// errors are not shown to the user.
var ErrBadArchive = errors.New("the package archive could not be read")

// ErrBadManifest is returned when the manifest is not valid JSON. The parser
// error is not shown to the user.
var ErrBadManifest = errors.New("the package manifest could not be read")

// archiveReadErr keeps the size guard error and hides any other reader error.
func archiveReadErr(err error) error {
	if errors.Is(err, errStreamTooLarge) {
		return errStreamTooLarge
	}
	return ErrBadArchive
}

// errStreamTooLarge is returned once the unpacked tar stream passes its cap.
var errStreamTooLarge = errors.New("the package archive unpacks too large")

// capReader fails once more than left bytes have been read through it. It caps
// the whole unpacked tar stream, so data the tar reader skips on its own (entry
// types that are not kept, PAX and long name records) still counts.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errStreamTooLarge
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// VerifiedPackage is a package archive that has passed every check: it holds the
// manifest and the payload files keyed by their archive path.
type VerifiedPackage struct {
	Manifest *Manifest
	Files    map[string][]byte
}

// VerifyAndExtract validates a downloaded archive end to end and returns its
// payload only when every check passes:
//   - the gzip sha256 matches expectedArchiveSHA (transport integrity),
//   - the manifest content hash matches its entries (payload binding),
//   - every manifest entry's bytes hash to its declared sha256, and no undeclared
//     file is present (no corrupt or extra file).
//
// The hashes come from the same registry as the archive, so they catch corruption
// and mismatched downloads, not a registry that serves altered content.
func VerifyAndExtract(archive []byte, expectedArchiveSHA string) (*VerifiedPackage, error) {
	if len(archive) == 0 {
		return nil, fmt.Errorf("the package archive is empty")
	}
	if int64(len(archive)) > maxArchiveBytes {
		return nil, fmt.Errorf("the package archive is too large")
	}
	// transport integrity: the bytes we got are the bytes the registry recorded. the
	// expected sha is required, so a registry cannot skip the check by omitting it.
	if expectedArchiveSHA == "" {
		return nil, fmt.Errorf("the package archive has no sha256 from the Bait Box")
	}
	gotSHA := sha256.Sum256(archive)
	if hex.EncodeToString(gotSHA[:]) != normHex(expectedArchiveSHA) {
		return nil, fmt.Errorf("the package archive does not match its sha256")
	}

	files, err := untar(archive)
	if err != nil {
		return nil, err
	}
	mj, ok := files[ManifestFileName]
	if !ok {
		return nil, fmt.Errorf("the package archive has no %s", ManifestFileName)
	}
	m, err := ParseManifest(mj)
	if err != nil {
		return nil, ErrBadManifest
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("the package manifest is invalid: %w", err)
	}

	// payload binding: the content hash must match the manifest entries.
	if ComputeContentHash(m.Entries) != normHex(m.ContentHash) {
		return nil, errors.New("the package content hash does not match its files")
	}

	// per entry integrity: every declared file is present and hashes correctly, and
	// the only non manifest files present are declared entries.
	declared := map[string]bool{ManifestFileName: true}
	for _, e := range m.Entries {
		declared[e.Path] = true
		b, ok := files[e.Path]
		if !ok {
			return nil, fmt.Errorf("the package file %q is missing from the archive", e.Path)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != normHex(e.SHA256) {
			return nil, fmt.Errorf("the package file %q does not match its checksum", e.Path)
		}
	}
	for name := range files {
		if !declared[name] {
			return nil, fmt.Errorf("the package archive has a file %q its manifest does not list", name)
		}
	}

	return &VerifiedPackage{Manifest: m, Files: files}, nil
}

// normHex lowercases a hex digest so a producer writing upper case hex still
// compares equal to the lower case hex the consumer computes.
func normHex(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// safeArchivePath reports whether a tar entry name is a strict relative path
// (see safepath.Check). A name that only contains two dots inside a segment,
// like "a..b.png", is fine.
func safeArchivePath(name string) bool {
	return safepath.Check(name) == nil
}

// untar reads a gzip+tar archive into a path->bytes map under the size, count and
// ratio guards. Every guard is checked while reading, so a bomb stops at the
// limit instead of after it is unpacked. Only regular files are kept; directory
// and other entries are skipped under their own small cap. Any entry
// path that is not strict, or a name seen twice even if only by letter case, is
// rejected.
func untar(archive []byte) (map[string][]byte, error) {
	// the unpacked budget is the smaller of the absolute cap and the ratio cap, with
	// a floor so a tiny archive of ordinary text is never refused.
	budget := int64(len(archive)) * maxCompressionRatio
	if budget < ratioFloorBytes {
		budget = ratioFloorBytes
	}
	if budget > maxTotalBytes {
		budget = maxTotalBytes
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, ErrBadArchive
	}
	defer gz.Close()
	tr := tar.NewReader(&capReader{r: gz, left: budget + streamSlackBytes})
	out := map[string][]byte{}
	folded := map[string]bool{}
	var total int64
	count, other := 0, 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, archiveReadErr(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			other++
			if other > maxOtherEntries {
				return nil, errors.New("the package archive has too many entries")
			}
			continue
		}
		count++
		if count > maxEntryCount {
			return nil, errors.New("the package archive has too many entries")
		}
		name := hdr.Name
		if !safeArchivePath(name) {
			return nil, fmt.Errorf("the package archive has an unsafe file path %q", name)
		}
		if folded[safepath.FoldKey(name)] {
			return nil, fmt.Errorf("the package archive has the file %q more than once", name)
		}
		folded[safepath.FoldKey(name)] = true
		// the per entry ceiling is the smallest of the file cap, the manifest cap and
		// what is left of the total budget, so reading stops as soon as any is hit.
		limit := int64(maxEntryBytes)
		if name == ManifestFileName && limit > maxManifestBytes {
			limit = maxManifestBytes
		}
		if left := budget - total; left < limit {
			limit = left
		}
		// do not trust hdr.Size for allocation (it is attacker controlled and may be
		// negative or huge); copy with a hard ceiling and let the buffer grow bounded.
		var w bytes.Buffer
		n, err := io.CopyN(&w, tr, limit+1)
		if err != nil && err != io.EOF {
			return nil, archiveReadErr(err)
		}
		if n > limit {
			switch {
			case name == ManifestFileName && limit == maxManifestBytes:
				return nil, errors.New("the package manifest is too large")
			case limit == maxEntryBytes:
				return nil, fmt.Errorf("the package file %q is too large", name)
			default:
				return nil, errStreamTooLarge
			}
		}
		total += n
		out[name] = w.Bytes()
	}
	return out, nil
}
