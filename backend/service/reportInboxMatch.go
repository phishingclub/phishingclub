package service

import (
	"bytes"
	"encoding/base64"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/lure"
)

// Limits for parsing a reported email. Reported mail can be real phishing or
// malware, so it is parsed with fixed caps and never rendered or stored.
const (
	reportMaxDepth       = 5
	reportMaxParts       = 50
	reportMaxTextBytes   = 2 << 20 // decoded text read over all parts
	reportMaxCandidates  = 100
	reportMaxMessageIDs  = 5
	reportMaxHeaderBytes = 256 << 10
	// reportMaxLureLinks caps the links read for a lure code per email
	reportMaxLureLinks = 10
	// reportMaxLurePath is how far the path of one link is read, a lure link is
	// far shorter
	reportMaxLurePath = 2048
	// reportMinLureLength is the shortest lure code used to match a report. A
	// shorter code is easier to guess, so a report is not matched on it.
	reportMinLureLength = 10
)

var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// hostPattern finds the host after a "//", also inside a link that a link
// rewriter wrapped in its own link
var hostPattern = regexp.MustCompile(`//([A-Za-z0-9.-]{1,253})`)

// reportCandidates is what a reported email can be matched on, in the order the
// matches are tried
type reportCandidates struct {
	// MessageIDs are Message-ID headers without angle brackets, the original
	// email first and then any email nested inside it
	MessageIDs []string
	// RecipientIDs are UUIDs found in headers, links and text. They are only
	// candidates, a lookup decides if one is a campaign recipient.
	RecipientIDs []uuid.UUID
	// LureLinks are links whose last path segment can be a lure code, tried
	// last and only on a domain Phishing Club serves
	LureLinks []lureLink
}

// lureLink is a domain Phishing Club serves with the possible lure code from
// the path of a link on it
type lureLink struct {
	Domain *database.Domain
	Code   string
}

type candidateCollector struct {
	out       reportCandidates
	seenMsgID map[string]bool
	seenID    map[uuid.UUID]bool
	seenLure  map[string]bool
	// served are the domains Phishing Club serves by lower case name. Only
	// links on these are read for a lure code, nil reads none.
	served   map[string]*database.Domain
	parts    int
	textLeft int
}

// extractReportCandidates parses a raw email and collects match candidates
func extractReportCandidates(raw []byte, served map[string]*database.Domain) *reportCandidates {
	c := &candidateCollector{
		seenMsgID: map[string]bool{},
		seenID:    map[uuid.UUID]bool{},
		textLeft:  reportMaxTextBytes,
		served:    served,
	}
	c.message(bytes.NewReader(raw), 0)
	return &c.out
}

// contentCandidates collects match candidates from the headers and body that
// Graph returns for a message
func contentCandidates(m *graphMessageContent, served map[string]*database.Domain) *reportCandidates {
	c := &candidateCollector{
		seenMsgID: map[string]bool{},
		seenID:    map[uuid.UUID]bool{},
		textLeft:  reportMaxTextBytes,
		served:    served,
	}
	// the message's own Message-ID is not a candidate, a forward always gets a
	// new one and the original is never matched on itself
	h := textproto.MIMEHeader{}
	for _, kv := range m.InternetMessageHeaders {
		h.Add(kv.Name, kv.Value)
	}
	c.headers(h)
	c.scanRaw([]byte(m.Body.Content))
	return &c.out
}

// itemCandidates collects match candidates from an attached Outlook item
func itemCandidates(item *graphItem, served map[string]*database.Domain) *reportCandidates {
	content := &graphMessageContent{
		InternetMessageID:      item.InternetMessageID,
		InternetMessageHeaders: item.InternetMessageHeaders,
	}
	if item.Body != nil {
		content.Body.Content = item.Body.Content
	}
	return contentCandidates(content, served)
}

// message reads one RFC 5322 message: its headers and then its body
func (c *candidateCollector) message(r io.Reader, depth int) {
	if depth > reportMaxDepth {
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r, reportMaxMIMEBytes))
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		// not a parsable email, still look for ids in whatever text it holds
		c.scanRaw(raw)
		return
	}
	c.addMessageID(msg.Header.Get("Message-Id"))
	c.headers(textproto.MIMEHeader(msg.Header))
	c.part(textproto.MIMEHeader(msg.Header), msg.Body, depth)
}

// headers scans header values for UUIDs, this is how a custom header such as
// X-Campaign: {{.rID}} is matched
func (c *candidateCollector) headers(h textproto.MIMEHeader) {
	size := 0
	var sb strings.Builder
	for name, values := range h {
		if strings.EqualFold(name, "Message-Id") {
			continue
		}
		// a forward or reply names the original email here, which is a
		// Message-ID to look up, not a recipient id
		if strings.EqualFold(name, "In-Reply-To") || strings.EqualFold(name, "References") {
			for _, v := range values {
				c.addReferences(v)
			}
			continue
		}
		for _, v := range values {
			size += len(v)
			if size > reportMaxHeaderBytes {
				break
			}
			sb.WriteString(v)
			sb.WriteByte('\n')
		}
	}
	c.findIDs(sb.String())
}

// part handles one MIME part, recursing into multiparts and nested messages
func (c *candidateCollector) part(h textproto.MIMEHeader, body io.Reader, depth int) {
	if depth > reportMaxDepth || c.parts >= reportMaxParts {
		return
	}
	c.parts++
	mediaType, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	encoding := h.Get("Content-Transfer-Encoding")
	if strings.HasPrefix(mediaType, "text/") {
		c.textPart(encoding, body)
		return
	}
	body = decodeTransfer(encoding, body)
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(body, boundary)
		for c.parts < reportMaxParts {
			p, err := mr.NextRawPart()
			if err != nil {
				return
			}
			c.part(p.Header, p, depth+1)
		}
	case mediaType == "message/rfc822":
		c.message(body, depth+1)
	}
}

// textPart reads a text part and looks for UUIDs in the decoded text. When the
// transfer encoding is broken the raw text is scanned as well.
func (c *candidateCollector) textPart(encoding string, r io.Reader) {
	if c.textLeft <= 0 {
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r, int64(c.textLeft)))
	c.textLeft -= len(raw)
	decoded, err := io.ReadAll(decodeTransfer(encoding, bytes.NewReader(raw)))
	c.scan(string(decoded))
	if err != nil {
		c.scan(string(raw))
	}
}

// scanRaw looks for UUIDs in bytes that could not be parsed as an email
func (c *candidateCollector) scanRaw(raw []byte) {
	if c.textLeft <= 0 {
		return
	}
	if len(raw) > c.textLeft {
		raw = raw[:c.textLeft]
	}
	c.textLeft -= len(raw)
	c.scan(string(raw))
}

// scan looks for UUIDs in text, also after undoing HTML entities and percent
// encoding, which Safe Links and similar link rewriters add
func (c *candidateCollector) scan(s string) {
	c.findIDs(s)
	unescaped := percentDecode(percentDecode(html.UnescapeString(s)))
	if unescaped != s {
		c.findIDs(unescaped)
	}
}

func (c *candidateCollector) findIDs(s string) {
	c.findLureLinks(s)
	for _, m := range uuidPattern.FindAllString(s, -1) {
		if len(c.out.RecipientIDs) >= reportMaxCandidates {
			return
		}
		id, err := uuid.Parse(m)
		if err != nil || c.seenID[id] {
			continue
		}
		c.seenID[id] = true
		c.out.RecipientIDs = append(c.out.RecipientIDs, id)
	}
}

// findLureLinks collects the possible lure codes of links on domains Phishing
// Club serves. A link is found by the "//host" in it, also when a link rewriter
// wrapped it, and the code is the last segment of the path after the host. Only
// codes long enough to not be guessed are kept, and only served domains count
// toward the cap.
func (c *candidateCollector) findLureLinks(s string) {
	if len(c.served) == 0 {
		return
	}
	for off := 0; off < len(s) && len(c.out.LureLinks) < reportMaxLureLinks; {
		m := hostPattern.FindStringSubmatchIndex(s[off:])
		if m == nil {
			return
		}
		hostEnd := off + m[3]
		host := strings.ToLower(strings.TrimSuffix(s[off+m[2]:hostEnd], "."))
		off = hostEnd
		domain, ok := c.served[host]
		if !ok || hostEnd >= len(s) || s[hostEnd] != '/' {
			continue
		}
		// the path runs to the end of the link: whitespace, quotes, markup, a
		// query or a fragment, or the parameters a link rewriter adds after a
		// wrapped link. A generated lure link has none of these.
		end := hostEnd
		for end < len(s) && end-hostEnd < reportMaxLurePath && !strings.ContainsRune(" \t\r\n\"'<>`?#&|;", rune(s[end])) {
			end++
		}
		// the next search starts after this path, so every byte is read once
		off = end
		// the code is the whole last path segment, without punctuation after a
		// link in text or the end marker some link rewriters add. A segment with
		// anything else, such as a file name, is not a code.
		path := strings.TrimRight(s[hostEnd:end], "/")
		code := strings.TrimRight(path[strings.LastIndex(path, "/")+1:], "_.,:!)]}")
		if len(code) < reportMinLureLength || len(code) > lure.MaxLength || !isLureAlphabet(code) {
			continue
		}
		key := host + "/" + code
		if c.seenLure == nil {
			c.seenLure = map[string]bool{}
		}
		if c.seenLure[key] {
			continue
		}
		c.seenLure[key] = true
		c.out.LureLinks = append(c.out.LureLinks, lureLink{Domain: domain, Code: code})
	}
}

// isLureAlphabet tells if a string only has letters and digits, as generated
// lure codes do
func isLureAlphabet(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')) {
			return false
		}
	}
	return true
}

// addReferences adds the Message-IDs of a References or In-Reply-To value. It
// walks the value one word at a time and stops once enough ids are kept, so a
// huge header in a sent email costs no extra memory.
func (c *candidateCollector) addReferences(v string) {
	for len(v) > 0 && len(c.out.MessageIDs) < reportMaxMessageIDs {
		v = strings.TrimLeft(v, " \t\r\n")
		end := strings.IndexAny(v, " \t\r\n")
		if end < 0 {
			end = len(v)
		}
		if end > 0 {
			c.addMessageID(v[:end])
		}
		v = v[end:]
	}
}

func (c *candidateCollector) addMessageID(v string) {
	v = normalizeMessageID(v)
	if v == "" || c.seenMsgID[v] || len(c.out.MessageIDs) >= reportMaxMessageIDs {
		return
	}
	c.seenMsgID[v] = true
	c.out.MessageIDs = append(c.out.MessageIDs, v)
}

// normalizeMessageID trims space and angle brackets from a Message-ID header
func normalizeMessageID(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "<")
	v = strings.TrimSuffix(v, ">")
	v = strings.TrimSpace(v)
	// the same limit as the stored column
	if len(v) > 512 {
		return ""
	}
	return v
}

// decodeTransfer undoes base64 and quoted printable transfer encoding
func decodeTransfer(encoding string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &base64Cleaner{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// base64Cleaner drops whitespace that some senders put in base64 bodies, which
// the standard decoder only tolerates as line breaks
type base64Cleaner struct {
	r io.Reader
}

func (b *base64Cleaner) Read(p []byte) (int, error) {
	for {
		n, err := b.r.Read(p)
		j := 0
		for i := 0; i < n; i++ {
			switch p[i] {
			case ' ', '\t', '\r', '\n':
				continue
			}
			p[j] = p[i]
			j++
		}
		// a read that held only whitespace is retried, as long as the source has more
		if j == 0 && n > 0 && err == nil {
			continue
		}
		return j, err
	}
}

// percentDecode decodes valid %XX sequences and leaves anything else as is
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			sb.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func unhex(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	default:
		return b - 'A' + 10
	}
}
