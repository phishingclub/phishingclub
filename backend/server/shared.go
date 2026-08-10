package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/lure"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// LastPathSegment returns the final segment of a decoded request path, with any
// trailing slash trimmed. Mail clients and message previews append one, so
// /account/4H7K9QM2XR3T/ must resolve the same as the link without it.
//
// Traversal is refused rather than cleaned, because a segment needing a clean is
// never a lure code. lure.IsCandidate refuses a doubled dot on the same terms,
// so a code that can be stored can always be resolved back.
func LastPathSegment(path string) (string, bool) {
	if path == "" || strings.Contains(path, "..") {
		return "", false
	}
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return "", false
	}
	index := strings.LastIndex(trimmed, "/")
	if index < 0 {
		return trimmed, true
	}
	segment := trimmed[index+1:]
	if segment == "" {
		return "", false
	}
	return segment, true
}

// lureCodeFromPath returns the trailing path segment of a request URL when it
// could be a lure code.
//
// URL.Path is already decoded, so an encoded separator has become a real one by
// the time it is read and would silently change which segment is last. RawPath
// is the only place that separator is still visible.
//
// Only the separator is refused, not every re encoding. A code may carry
// characters Go escapes in a path, such as a bracket or anything non ASCII, and
// each sets RawPath on its own. Refusing on RawPath alone would make all of
// those unreachable while IsValidCustom still accepted them.
func lureCodeFromPath(u *url.URL) (string, bool) {
	if u == nil {
		return "", false
	}
	if strings.Contains(u.RawPath, "%2f") || strings.Contains(u.RawPath, "%2F") {
		return "", false
	}
	segment, ok := LastPathSegment(u.Path)
	if !ok || !lure.IsCandidate(segment) {
		return "", false
	}
	return segment, true
}

// TrimLastPathSegment removes the final segment of a path, taking a consumed
// lure code back out of the URL before the request is forwarded on.
func TrimLastPathSegment(path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	index := strings.LastIndex(trimmed, "/")
	if index <= 0 {
		return "/"
	}
	return trimmed[:index]
}

// LureMatch records how a request identified its recipient, so a caller can undo
// whatever carried the identifier before passing the request on.
type LureMatch struct {
	// ParamName is the query parameter that matched. Empty when the identifier
	// came from the path.
	ParamName string
	// PathSegment is the trailing path segment that matched. Empty when the
	// identifier came from the query.
	PathSegment string
}

// GetCampaignRecipientFromURLParams resolves the campaign recipient a request
// belongs to.
//
// Three forms are accepted, in this order:
//
//   - a query parameter holding a campaign recipient UUID, the original form
//   - a query parameter holding a lure code, so an operator set code such as
//     special-42 works as ?id=special-42 too
//   - the last path segment as a lure code, https://example.com/acc/4H7K9QM2XR3T
//
// All three are accepted whatever the campaign's mode. The mode only decides
// which form is emitted, so changing it never breaks a delivered link.
//
// Query beats path. A query identifier states which recipient a request belongs
// to, while a path segment only looks like a code: a template whose URL path
// ends in /careers collides with any custom code holding that string.
//
// Both code forms resolve only against campaigns reachable on the serving
// domain. A code is short enough to guess, so without that a guess made on any
// domain would reach a recipient of any campaign, including another company's.
//
// allowLureCodeLookup turns the code forms off, leaving only the UUID. The proxy
// does that once a session cookie exists, because there every path and query
// mirrors the target site and the recipient is already known. A UUID cannot
// collide with target site content by accident, so it stays on.
func GetCampaignRecipientFromURLParams(
	ctx context.Context,
	req *http.Request,
	identifierRepo *repository.Identifier,
	campaignRecipientRepo *repository.CampaignRecipient,
	domain *database.Domain,
	allowLureCodeLookup bool,
) (*model.CampaignRecipient, LureMatch, error) {
	// get all identifiers
	identifiers, err := identifierRepo.GetAll(ctx, &repository.IdentifierOption{})
	if err != nil {
		return nil, LureMatch{}, err
	}

	// a code resolves only against the serving domain, so without one there is
	// nothing to match it to
	lookupCodes := allowLureCodeLookup && domain != nil

	query := req.URL.Query()
	var matchingUUIDParams []struct {
		name string
		id   *uuid.UUID
	}
	var matchingCodeParams []struct {
		name  string
		value string
	}

	// split matching identifier params by whether they carry a UUID or a code
	for _, identifier := range identifiers.Rows {
		name := identifier.Name.MustGet()
		if !query.Has(name) {
			continue
		}
		value := query.Get(name)
		if id, err := uuid.Parse(value); err == nil {
			matchingUUIDParams = append(matchingUUIDParams, struct {
				name string
				id   *uuid.UUID
			}{name: name, id: &id})
			continue
		}
		if lookupCodes && lure.IsCandidate(value) {
			matchingCodeParams = append(matchingCodeParams, struct {
				name  string
				value string
			}{name: name, value: value})
		}
	}

	// check each matching parameter to find a valid campaign recipient
	for _, param := range matchingUUIDParams {
		campaignRecipient, err := campaignRecipientRepo.GetByCampaignRecipientID(ctx, param.id)
		if err == nil && campaignRecipient != nil {
			return campaignRecipient, LureMatch{ParamName: param.name}, nil
		}
	}
	for _, param := range matchingCodeParams {
		campaignRecipient, err := campaignRecipientRepo.GetByLureCodeOnDomain(ctx, param.value, domain)
		if err == nil && campaignRecipient != nil {
			return campaignRecipient, LureMatch{ParamName: param.name}, nil
		}
	}

	// nothing in the query matched, so read the last path segment as a code
	if lookupCodes {
		if segment, ok := lureCodeFromPath(req.URL); ok {
			campaignRecipient, err := campaignRecipientRepo.GetByLureCodeOnDomain(ctx, segment, domain)
			if err == nil && campaignRecipient != nil {
				return campaignRecipient, LureMatch{PathSegment: segment}, nil
			}
		}
	}

	return nil, LureMatch{}, nil
}
