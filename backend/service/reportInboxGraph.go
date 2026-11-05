package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The Graph and login hosts are fixed. No part of these urls comes from user
// input except the tenant, mailbox and message ids, which are validated and
// path escaped.
const (
	graphBaseURL = "https://graph.microsoft.com/v1.0"
	loginBaseURL = "https://login.microsoftonline.com"

	graphMaxJSONBytes = 5 << 20
	// reportMaxMIMEBytes caps one reported email. A larger one is skipped.
	reportMaxMIMEBytes = 10 << 20
)

// graphError is an error response from Graph or the token endpoint
type graphError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *graphError) Error() string {
	msg := e.Message
	if len(msg) > 300 {
		msg = msg[:300]
	}
	if e.Code != "" {
		return fmt.Sprintf("microsoft graph returned %d %s: %s", e.Status, e.Code, msg)
	}
	return fmt.Sprintf("microsoft graph returned %d: %s", e.Status, msg)
}

// graphMailClient reads a mailbox through Microsoft Graph
type graphMailClient struct {
	HTTP      *http.Client
	GraphBase string
	LoginBase string
}

func newGraphMailClient() *graphMailClient {
	return &graphMailClient{
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			// never follow a redirect away from the fixed hosts
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		GraphBase: graphBaseURL,
		LoginBase: loginBaseURL,
	}
}

type graphMessage struct {
	ID               string    `json:"id"`
	ReceivedDateTime time.Time `json:"receivedDateTime"`
	HasAttachments   bool      `json:"hasAttachments"`
	IsDraft          bool      `json:"isDraft"`
	From             struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"from"`
}

type graphAttachment struct {
	ODataType   string `json:"@odata.type"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

// appToken gets an app only Graph token with the client credentials flow
func (g *graphMailClient) appToken(ctx context.Context, tenantID, clientID, secret string) (string, time.Time, error) {
	tokenURL := g.LoginBase + "/" + url.PathEscape(tenantID) + "/oauth2/v2.0/token"
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {secret},
		"scope":         {"https://graph.microsoft.com/.default"},
		"grant_type":    {"client_credentials"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, graphMaxJSONBytes))
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		// the description starts with an AADSTS code that tells the admin what is wrong
		desc := e.Description
		if i := strings.Index(desc, "\r\n"); i > 0 {
			desc = desc[:i]
		}
		return "", time.Time{}, &graphError{Status: resp.StatusCode, Code: e.Error, Message: desc}
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("token response had no access token")
	}
	return tok.AccessToken, time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second), nil
}

// get performs a Graph GET and returns the body, capped at max bytes. tooLarge is
// true when the body was cut at the cap.
func (g *graphMailClient) get(ctx context.Context, token, path string, max int64) (body []byte, tooLarge bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.GraphBase+path, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// immutable ids stay the same when a message is moved between folders
	req.Header.Set("Prefer", `IdType="ImmutableId"`)
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("graph request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		ge := &graphError{Status: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			if secs, convErr := strconv.Atoi(resp.Header.Get("Retry-After")); convErr == nil && secs > 0 {
				ge.RetryAfter = time.Duration(secs) * time.Second
			}
		}
		return nil, false, ge
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > max {
		return nil, true, nil
	}
	return body, false, nil
}

func mailboxPath(mailbox string) string {
	return "/users/" + url.PathEscape(mailbox)
}

// graphMessageFields are the fields read when listing messages
const graphMessageFields = "id,receivedDateTime,hasAttachments,isDraft,from"

// messagesFirstPage is the path of the first page of messages in every folder
// of the mailbox received at or after since, oldest first
func messagesFirstPage(mailbox string, since time.Time, top int) string {
	q := url.Values{}
	q.Set("$select", graphMessageFields)
	q.Set("$filter", "receivedDateTime ge "+since.UTC().Format(time.RFC3339))
	q.Set("$orderby", "receivedDateTime asc")
	q.Set("$top", strconv.Itoa(top))
	return mailboxPath(mailbox) + "/messages?" + q.Encode()
}

// getMessage returns the list fields of one message, used to read a message
// that failed before again by its id
func (g *graphMailClient) getMessage(ctx context.Context, token, mailbox, messageID string) (*graphMessage, error) {
	q := url.Values{}
	q.Set("$select", graphMessageFields)
	body, tooLarge, err := g.get(ctx, token, mailboxPath(mailbox)+"/messages/"+url.PathEscape(messageID)+"?"+q.Encode(), graphMaxJSONBytes)
	if err != nil {
		return nil, err
	}
	if tooLarge {
		return nil, errors.New("message too large")
	}
	var m graphMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("invalid message: %w", err)
	}
	return &m, nil
}

// listMessages returns one page of messages and the path of the next page,
// empty when there is none. The next link from Graph is only followed when it
// points at the same Graph base url.
func (g *graphMailClient) listMessages(ctx context.Context, token, path string) ([]graphMessage, string, error) {
	body, _, err := g.get(ctx, token, path, graphMaxJSONBytes)
	if err != nil {
		return nil, "", err
	}
	var res struct {
		Value    []graphMessage `json:"value"`
		NextLink string         `json:"@odata.nextLink"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, "", fmt.Errorf("invalid message list: %w", err)
	}
	next := ""
	if res.NextLink != "" {
		if !strings.HasPrefix(res.NextLink, g.GraphBase+"/") {
			return nil, "", fmt.Errorf("unexpected next link from graph")
		}
		next = strings.TrimPrefix(res.NextLink, g.GraphBase)
	}
	return res.Value, next, nil
}

// listAttachments lists the attachments of a message without their content
func (g *graphMailClient) listAttachments(ctx context.Context, token, mailbox, messageID string) ([]graphAttachment, error) {
	q := url.Values{}
	q.Set("$select", "id,name,contentType,size")
	path := mailboxPath(mailbox) + "/messages/" + url.PathEscape(messageID) + "/attachments?" + q.Encode()
	body, _, err := g.get(ctx, token, path, graphMaxJSONBytes)
	if err != nil {
		return nil, err
	}
	var res struct {
		Value []graphAttachment `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("invalid attachment list: %w", err)
	}
	return res.Value, nil
}

// attachmentMIME returns the raw content of an attachment. For an attached
// Outlook item Graph returns it as MIME, so .msg never needs to be parsed here.
func (g *graphMailClient) attachmentMIME(ctx context.Context, token, mailbox, messageID, attachmentID string) ([]byte, bool, error) {
	path := mailboxPath(mailbox) + "/messages/" + url.PathEscape(messageID) +
		"/attachments/" + url.PathEscape(attachmentID) + "/$value"
	return g.get(ctx, token, path, reportMaxMIMEBytes)
}

// graphItem is an email attached to a message as an Outlook item
type graphItem struct {
	// ODataType is the kind of item, such as #microsoft.graph.message or
	// #microsoft.graph.event
	ODataType         string `json:"@odata.type"`
	InternetMessageID string `json:"internetMessageId"`
	From              struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"from"`
	InternetMessageHeaders []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"internetMessageHeaders"`
	Body *struct {
		Content string `json:"content"`
	} `json:"body"`
}

// itemAttachment returns the properties of an email attached as an Outlook item,
// limited to the given fields. The attached email's own files are never part of
// the response. When Graph does not accept the field list and the body is wanted,
// the default fields are returned, which include the body but still no files.
// Without the body only the item's type is asked for, so an item that is not an
// email can be passed over and an email stays an error to retry.
func (g *graphMailClient) itemAttachment(ctx context.Context, token, mailbox, messageID, attachmentID string, fields string) (*graphItem, error) {
	base := mailboxPath(mailbox) + "/messages/" + url.PathEscape(messageID) + "/attachments/" + url.PathEscape(attachmentID)
	fetch := func(expand string) (*graphItem, error) {
		q := url.Values{}
		q.Set("$expand", expand)
		body, tooLarge, err := g.get(ctx, token, base+"?"+q.Encode(), graphMaxJSONBytes)
		if err != nil {
			return nil, err
		}
		if tooLarge {
			return nil, nil
		}
		var res struct {
			Item *graphItem `json:"item"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, fmt.Errorf("invalid attachment: %w", err)
		}
		return res.Item, nil
	}
	// the item is declared as an outlookItem, so message fields are only
	// accepted with the message type in front of each of them
	parts := strings.Split(fields, ",")
	for i, f := range parts {
		parts[i] = "microsoft.graph.message/" + strings.TrimSpace(f)
	}
	item, err := fetch("microsoft.graph.itemattachment/item($select=" + strings.Join(parts, ",") + ")")
	var ge *graphError
	if errors.As(err, &ge) && ge.Status == http.StatusBadRequest {
		// the full item includes the body, so it is only fetched when the body
		// is wanted anyway. The first check of a reported email reads its
		// sender only, so then only the type of the item is asked for.
		if strings.Contains(fields, "body") {
			return fetch("microsoft.graph.itemattachment/item")
		}
		typed, typeErr := fetch("microsoft.graph.itemattachment/item($select=id)")
		if typeErr != nil || typed == nil {
			return nil, err
		}
		// only a known type that is not an email is passed over, anything else
		// stays an error so the message is retried
		if typed.ODataType != "" && !strings.Contains(strings.ToLower(typed.ODataType), "message") {
			return typed, nil
		}
		return nil, err
	}
	return item, err
}

// graphMessageContent is the headers and text body of a message, without any
// attachment content
type graphMessageContent struct {
	InternetMessageID      string `json:"internetMessageId"`
	InternetMessageHeaders []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"internetMessageHeaders"`
	Body struct {
		Content string `json:"content"`
	} `json:"body"`
}

// messageContent returns the headers and body of a message. Attachments are not
// part of the response, so this never downloads attached files.
func (g *graphMailClient) messageContent(ctx context.Context, token, mailbox, messageID string) (*graphMessageContent, error) {
	q := url.Values{}
	q.Set("$select", "internetMessageId,internetMessageHeaders,body")
	path := mailboxPath(mailbox) + "/messages/" + url.PathEscape(messageID) + "?" + q.Encode()
	body, tooLarge, err := g.get(ctx, token, path, graphMaxJSONBytes)
	if err != nil {
		return nil, err
	}
	if tooLarge {
		return nil, nil
	}
	var m graphMessageContent
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("invalid message: %w", err)
	}
	return &m, nil
}

// canRead tells if the token can list messages in a mailbox
func (g *graphMailClient) canRead(ctx context.Context, token, mailbox string) error {
	_, _, err := g.get(ctx, token, mailboxPath(mailbox)+"/messages?%24top=1&%24select=id", graphMaxJSONBytes)
	return err
}

// canOpenInbox tells if the token can open the inbox folder of a mailbox. Only
// folder metadata is requested, never a message.
func (g *graphMailClient) canOpenInbox(ctx context.Context, token, mailbox string) error {
	_, _, err := g.get(ctx, token, mailboxPath(mailbox)+"/mailFolders/inbox?%24select=id", graphMaxJSONBytes)
	return err
}

// isAccessRefused tells if Graph refused access to an existing mailbox
func isAccessRefused(err error, delegated bool) bool {
	var ge *graphError
	if !errors.As(err, &ge) {
		return false
	}
	if ge.Status == http.StatusForbidden {
		return true
	}
	// a delegated account that can not open another mailbox gets item not
	// found, while a missing mailbox is an invalid user. An app is refused
	// with a 403.
	return delegated && ge.Status == http.StatusNotFound && ge.Code == "ErrorItemNotFound"
}

// isAccessDenied tells if a Graph error means the mailbox can not be read with
// this token, as opposed to a network or server problem
func isAccessDenied(err error) bool {
	var ge *graphError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.Status == http.StatusForbidden || ge.Status == http.StatusNotFound || ge.Status == http.StatusUnauthorized
}

// tokenClaims reads the roles and scopes from a Graph access token. The token is
// not verified, it is only inspected to warn about permissions that are broader
// than one mailbox.
func tokenClaims(token string) (roles []string, scopes []string) {
	c := readTokenClaims(token)
	if c.Scp != "" {
		scopes = strings.Fields(c.Scp)
	}
	return c.Roles, scopes
}

// signedInAddress returns the address of the account a delegated token belongs
// to, empty for an app token
func signedInAddress(token string) string {
	c := readTokenClaims(token)
	if c.UPN != "" {
		return c.UPN
	}
	return c.PreferredUsername
}

type graphTokenClaims struct {
	Roles             []string `json:"roles"`
	Scp               string   `json:"scp"`
	UPN               string   `json:"upn"`
	PreferredUsername string   `json:"preferred_username"`
}

// readTokenClaims decodes the claims of a token without verifying it
func readTokenClaims(token string) graphTokenClaims {
	var claims graphTokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims
	}
	_ = json.Unmarshal(payload, &claims)
	return claims
}

// broadMailRoles returns the app roles in a token that give access to mail in
// every mailbox of the tenant. Exchange RBAC for Applications grants do not show
// up as roles, so with the intended setup this is empty.
func broadMailRoles(roles []string) []string {
	out := []string{}
	for _, r := range roles {
		if strings.HasPrefix(r, "Mail.") || strings.HasPrefix(r, "MailboxSettings.") || r == "full_access_as_app" {
			out = append(out, r)
		}
	}
	return out
}
