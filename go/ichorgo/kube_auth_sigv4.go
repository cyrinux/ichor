package ichorgo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AWS Signature Version 4, the parts EKS tokens and STS need: signing a request's headers,
// and presigning a URL. Written out rather than taken from the AWS SDK, which would weigh
// on the app for two calls.

type awsCredentials struct {
	accessKeyID, secretAccessKey, sessionToken string
}

const (
	sigV4Algorithm  = "AWS4-HMAC-SHA256"
	sigV4TimeFormat = "20060102T150405Z"
	sigV4DateFormat = "20060102"
	emptySHA256     = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))

	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// sigV4Key derives the signing key of a day, region and service.
func sigV4Key(secret, date, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), date)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)

	return hmacSHA256(k, "aws4_request")
}

// sigV4Escape is AWS's URI encoding: RFC 3986 unreserved characters kept, the rest %XX.
func sigV4Escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var parts []string

	for _, k := range keys {
		values := slices.Clone(q[k])
		sort.Strings(values)

		for _, v := range values {
			parts = append(parts, sigV4Escape(k)+"="+sigV4Escape(v))
		}
	}

	return strings.Join(parts, "&")
}

// canonicalHeaders returns the canonical header block and the signed header list.
func canonicalHeaders(h http.Header, host string) (string, string) {
	values := map[string]string{"host": host}

	for k, v := range h {
		values[strings.ToLower(k)] = strings.Join(v, ",")
	}

	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}

	sort.Strings(names)

	var b strings.Builder
	for _, k := range names {
		b.WriteString(k + ":" + strings.Join(strings.Fields(values[k]), " ") + "\n")
	}

	return b.String(), strings.Join(names, ";")
}

func canonicalPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}

	return p
}

// sigV4Sign signs req (its body hashed as payloadHash) in place, adding X-Amz-Date, the
// session token if any, and Authorization.
func sigV4Sign(req *http.Request, payloadHash string, creds awsCredentials, region, service string, now time.Time) {
	now = now.UTC()
	req.Header.Set("X-Amz-Date", now.Format(sigV4TimeFormat))

	if creds.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.sessionToken)
	}

	headers, signed := canonicalHeaders(req.Header, req.URL.Host)
	canonical := strings.Join([]string{req.Method, canonicalPath(req.URL), canonicalQuery(req.URL.Query()), headers, signed, payloadHash}, "\n")

	scope := now.Format(sigV4DateFormat) + "/" + region + "/" + service + "/aws4_request"
	toSign := strings.Join([]string{sigV4Algorithm, now.Format(sigV4TimeFormat), scope, sha256Hex([]byte(canonical))}, "\n")
	signature := hex.EncodeToString(hmacSHA256(sigV4Key(creds.secretAccessKey, now.Format(sigV4DateFormat), region, service), toSign))

	req.Header.Set("Authorization", sigV4Algorithm+" Credential="+creds.accessKeyID+"/"+scope+", SignedHeaders="+signed+", Signature="+signature)
}

// sigV4Presign returns u (a GET) presigned for expires, with extra headers signed (they must
// be sent with the URL: EKS's x-k8s-aws-id). payloadHash is emptySHA256 for STS.
func sigV4Presign(u *url.URL, headers http.Header, creds awsCredentials, region, service, payloadHash string, expires time.Duration, now time.Time) string {
	now = now.UTC()
	scope := now.Format(sigV4DateFormat) + "/" + region + "/" + service + "/aws4_request"

	canonHeaders, signed := canonicalHeaders(headers, u.Host)

	q := u.Query()
	q.Set("X-Amz-Algorithm", sigV4Algorithm)
	q.Set("X-Amz-Credential", creds.accessKeyID+"/"+scope)
	q.Set("X-Amz-Date", now.Format(sigV4TimeFormat))
	q.Set("X-Amz-Expires", strconv.Itoa(int(expires/time.Second)))
	q.Set("X-Amz-SignedHeaders", signed)

	if creds.sessionToken != "" {
		q.Set("X-Amz-Security-Token", creds.sessionToken)
	}

	canonical := strings.Join([]string{http.MethodGet, canonicalPath(u), canonicalQuery(q), canonHeaders, signed, payloadHash}, "\n")
	toSign := strings.Join([]string{sigV4Algorithm, now.Format(sigV4TimeFormat), scope, sha256Hex([]byte(canonical))}, "\n")
	q.Set("X-Amz-Signature", hex.EncodeToString(hmacSHA256(sigV4Key(creds.secretAccessKey, now.Format(sigV4DateFormat), region, service), toSign)))

	out := *u
	out.RawQuery = canonicalQuery(q)

	return out.String()
}
