package service

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
)

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func TestExtractReportCandidates_MessageIDAndHeader(t *testing.T) {
	rid := uuid.New()
	raw := "Message-ID: <abc-123@sender.example>\r\n" +
		"X-Tracking: " + rid.String() + "\r\n" +
		"Subject: hello\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"no ids here\r\n"
	c := extractReportCandidates([]byte(raw), nil)
	if len(c.MessageIDs) != 1 || c.MessageIDs[0] != "abc-123@sender.example" {
		t.Fatalf("message ids: %v", c.MessageIDs)
	}
	if !containsID(c.RecipientIDs, rid) {
		t.Fatalf("header id not found: %v", c.RecipientIDs)
	}
}

func TestExtractReportCandidates_SafeLinksQuotedPrintable(t *testing.T) {
	rid := uuid.New()
	inner := "https://lure.example/login?id=" + rid.String()
	// safe links percent encodes the original url, then the body is quoted printable
	// with a soft line break in the middle of the id
	wrapped := "https://eur01.safelinks.protection.outlook.com/?url=" +
		strings.ReplaceAll(strings.ReplaceAll(inner, ":", "%3A"), "/", "%2F") + "&amp;data=05"
	// a quoted printable encoder writes = as =3D and wraps long lines
	qp := strings.ReplaceAll(wrapped, "=", "=3D")
	qp = strings.Replace(qp, rid.String()[:10], rid.String()[:5]+"=\r\n"+rid.String()[5:10], 1)
	raw := "Message-ID: <x@y.example>\r\n" +
		"Content-Type: multipart/alternative; boundary=\"b1\"\r\n\r\n" +
		"--b1\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"<a href=3D\"" + qp + "\">click</a>\r\n" +
		"--b1--\r\n"
	c := extractReportCandidates([]byte(raw), nil)
	if !containsID(c.RecipientIDs, rid) {
		t.Fatalf("id behind safe links not found: %v", c.RecipientIDs)
	}
}

func TestExtractReportCandidates_Base64AndNested(t *testing.T) {
	rid := uuid.New()
	inner := "Message-ID: <inner@sender.example>\r\n" +
		"Content-Type: text/html\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString([]byte(`<img src="https://t.example/p.png?rid=`+rid.String()+`">`)) + "\r\n"
	raw := "Message-ID: <outer@company.example>\r\n" +
		"Content-Type: multipart/mixed; boundary=\"m\"\r\n\r\n" +
		"--m\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"see attached\r\n" +
		"--m\r\n" +
		"Content-Type: message/rfc822\r\n\r\n" +
		inner +
		"--m--\r\n"
	c := extractReportCandidates([]byte(raw), nil)
	if len(c.MessageIDs) != 2 || c.MessageIDs[0] != "outer@company.example" || c.MessageIDs[1] != "inner@sender.example" {
		t.Fatalf("message ids: %v", c.MessageIDs)
	}
	if !containsID(c.RecipientIDs, rid) {
		t.Fatalf("id in nested base64 body not found: %v", c.RecipientIDs)
	}
}

func TestExtractReportCandidates_Caps(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("Content-Type: text/plain\r\n\r\n")
	for i := 0; i < reportMaxCandidates*3; i++ {
		sb.WriteString(uuid.NewString())
		sb.WriteString(" ")
	}
	c := extractReportCandidates([]byte(sb.String()), nil)
	if len(c.RecipientIDs) != reportMaxCandidates {
		t.Fatalf("expected %d candidates, got %d", reportMaxCandidates, len(c.RecipientIDs))
	}

	// deep nesting stops at the depth cap without error
	deep := "Content-Type: text/plain\r\n\r\nend\r\n"
	for i := 0; i < reportMaxDepth*3; i++ {
		deep = "Content-Type: message/rfc822\r\n\r\n" + deep
	}
	_ = extractReportCandidates([]byte(deep), nil)
}

func TestExtractReportCandidates_Garbage(t *testing.T) {
	rid := uuid.New()
	for _, raw := range []string{
		"",
		"\x00\x01\x02",
		"Content-Type: multipart/mixed\r\n\r\nno boundary " + rid.String(),
		"Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nunterminated " + rid.String(),
		"not an email at all but has " + rid.String(),
	} {
		_ = extractReportCandidates([]byte(raw), nil)
	}
	c := extractReportCandidates([]byte("not an email at all but has "+rid.String()), nil)
	if !containsID(c.RecipientIDs, rid) {
		t.Fatalf("id in unparsable text not found")
	}
}

func TestNormalizeMessageID(t *testing.T) {
	cases := map[string]string{
		" <a@b> ": "a@b",
		"a@b":     "a@b",
		"<>":      "",
		"":        "",
	}
	for in, want := range cases {
		if got := normalizeMessageID(in); got != want {
			t.Fatalf("normalizeMessageID(%q) = %q, want %q", in, got, want)
		}
	}
	// a uuid with a long sending domain still fits
	long := strings.Repeat("a", 300)
	if normalizeMessageID("<"+long+">") != long {
		t.Fatalf("an id up to the column size must be kept")
	}
	if normalizeMessageID("<"+strings.Repeat("a", 600)+">") != "" {
		t.Fatalf("too long id must be dropped")
	}
}

func TestBroadMailRoles(t *testing.T) {
	got := broadMailRoles([]string{"User.Read.All", "Mail.Read", "Mail.ReadWrite"})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if len(broadMailRoles(nil)) != 0 {
		t.Fatalf("expected none")
	}
}

func TestFindLureLinks_OnlyServedDomainsAndLongCodes(t *testing.T) {
	served := map[string]*database.Domain{
		"lure.example":  {Name: "lure.example"},
		"other.example": {Name: "other.example"},
		"share.example": {Name: "share.example"},
	}
	c := &candidateCollector{seenMsgID: map[string]bool{}, seenID: map[uuid.UUID]bool{}, textLeft: reportMaxTextBytes, served: served}
	c.scan(`<a href="https://linkedin.example/company/ContosoCorp1">a</a>
<a href="https://lure.example/login/K7Q2M9XR4TW8">a</a>
<a href="https://lure.example/login/abc123">short</a>
https://urldefense.example/v3/__https://other.example/p/ZZ9QK2M7XR4T__;!!x$
<a href="https://safelinks.example/?url=https%3A%2F%2Fshare.example%2F%3Ab%3A%2Fg%2FAB12CD34EF56&data=1">wrapped</a>
see https://lure.example/x/QQ7RT2M9XK4W.
<a href="https://safelinks.example/?url=https%3A%2F%2Flure.example%2Flogin%2FMM4KQ7RT2X9W&amp;data=05%7C02&amp;sdata=AbC%2FdEfGhIjKlMn%3D&amp;reserved=0">sl</a>
<img src="https://lure.example/img/CompanyLogo2024.png">`)
	got := map[string]bool{}
	for _, l := range c.out.LureLinks {
		got[l.Domain.Name+"/"+l.Code] = true
	}
	for _, want := range []string{"lure.example/K7Q2M9XR4TW8", "other.example/ZZ9QK2M7XR4T", "share.example/AB12CD34EF56", "lure.example/QQ7RT2M9XK4W", "lure.example/MM4KQ7RT2X9W"} {
		if !got[want] {
			t.Fatalf("missing %s, got %v", want, got)
		}
	}
	if len(got) != 5 {
		t.Fatalf("only long codes on served domains may be kept, got %v", got)
	}
}

func TestFindLureLinks_LargeInputIsLinear(t *testing.T) {
	served := map[string]*database.Domain{"d.co": {Name: "d.co"}}
	text := strings.Repeat("//d.co/x", (2<<20)/8)
	c := &candidateCollector{seenMsgID: map[string]bool{}, seenID: map[uuid.UUID]bool{}, textLeft: reportMaxTextBytes, served: served}
	start := time.Now()
	c.findLureLinks(text)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("scanning 2 MB took %s, it must read each byte once", took)
	}
}
