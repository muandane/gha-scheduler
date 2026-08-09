package s3backend

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// S3Store implements ObjectStore against SeaweedFS (or any path-style S3 API) using stdlib + SigV4.
// No third-party AWS SDK — SigV4 signing is inlined for homelab SeaweedFS filer S3 gateway.
type S3Store struct {
	endpoint  string
	bucket    string
	region    string
	accessKey string
	secretKey string
	client    *http.Client
}

// S3Config configures a SeaweedFS/S3-compatible store.
type S3Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Client    *http.Client
}

// NewS3Store creates an S3Store. Region defaults to "us-east-1" when empty.
func NewS3Store(cfg S3Config) (*S3Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("s3backend: S3_ENDPOINT and S3_BUCKET are required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	return &S3Store{
		endpoint:  strings.TrimRight(cfg.Endpoint, "/"),
		bucket:    cfg.Bucket,
		region:    cfg.Region,
		accessKey: cfg.AccessKey,
		secretKey: cfg.SecretKey,
		client:    cfg.Client,
	}, nil
}

// Put uploads an object.
func (s *S3Store) Put(ctx context.Context, objectKey string, r io.Reader) (int64, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(objectKey), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if err := s.sign(req, hashHex(body)); err != nil {
		return 0, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("s3backend: put status %d: %s", resp.StatusCode, string(b))
	}
	return int64(len(body)), nil
}

// Get downloads an object.
func (s *S3Store) Get(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(objectKey), nil)
	if err != nil {
		return nil, 0, err
	}
	if err := s.sign(req, emptyHash); err != nil {
		return nil, 0, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, 0, fmt.Errorf("s3backend: get status %d", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

// List returns object keys with the given prefix (ListObjectsV2, paginated).
func (s *S3Store) List(ctx context.Context, prefix string) ([]string, error) {
	var all []string
	var continuation string
	for {
		keys, next, err := s.listPage(ctx, prefix, continuation)
		if err != nil {
			return nil, err
		}
		all = append(all, keys...)
		if next == "" {
			break
		}
		continuation = next
	}
	return all, nil
}

func (s *S3Store) listPage(ctx context.Context, prefix, continuation string) ([]string, string, error) {
	u, err := url.Parse(s.bucketURL())
	if err != nil {
		return nil, "", err
	}
	q := u.Query()
	q.Set("list-type", "2")
	q.Set("prefix", prefix)
	if continuation != "" {
		q.Set("continuation-token", continuation)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	if err := s.sign(req, emptyHash); err != nil {
		return nil, "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("s3backend: list status %d: %s", resp.StatusCode, string(b))
	}
	var parsed listObjectsV2Result
	if err := xml.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, "", err
	}
	keys := make([]string, 0, len(parsed.Contents))
	for _, c := range parsed.Contents {
		keys = append(keys, c.Key)
	}
	next := ""
	if parsed.IsTruncated && parsed.NextContinuationToken != "" {
		next = parsed.NextContinuationToken
	}
	return keys, next, nil
}

type listObjectsV2Result struct {
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
	Contents              []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

func (s *S3Store) objectURL(objectKey string) string {
	return s.bucketURL() + "/" + escapeKey(objectKey)
}

func (s *S3Store) bucketURL() string {
	return s.endpoint + "/" + s.bucket
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *S3Store) sign(req *http.Request, payloadHash string) error {
	if s.accessKey == "" && s.secretKey == "" {
		return nil
	}
	now := time.Now()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	host := req.URL.Host
	canonicalURI := req.URL.EscapedPath()
	canonicalQuery := req.URL.RawQuery
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	crHash := sha256.Sum256([]byte(canonicalRequest))
	credentialScope := dateStamp + "/" + s.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hex.EncodeToString(crHash[:]),
	}, "\n")

	signingKey := deriveSigningKey(s.secretKey, dateStamp, s.region, "s3")
	sig := hmacSum(signingKey, stringToSign)
	auth := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, credentialScope, signedHeaders, hex.EncodeToString(sig),
	)
	req.Header.Set("Authorization", auth)
	return nil
}

func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSum([]byte("AWS4"+secret), date)
	kRegion := hmacSum(kDate, region)
	kService := hmacSum(kRegion, service)
	return hmacSum(kService, "aws4_request")
}

func hmacSum(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
