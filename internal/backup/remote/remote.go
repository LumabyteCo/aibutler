package remote

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
	"sort"
	"strings"
	"time"

	"filippo.io/age"
)

// Provider identifies the remote backup destination type.
type Provider string

const (
	ProviderS3   Provider = "s3"
	ProviderHTTP Provider = "http"
)

// Config holds remote backup configuration.
type Config struct {
	Provider   Provider
	Endpoint   string
	Bucket     string
	AccessKey  string
	SecretKey  string
	Region     string
	EncryptKey string // age passphrase for encryption (optional)
}

// RemoteFile describes a file stored remotely.
type RemoteFile struct {
	Name     string
	Size     int64
	Modified time.Time
}

// Client performs encrypted remote backups.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient creates a remote backup client.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Encrypt encrypts data using age with the configured passphrase.
func Encrypt(data []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return data, nil
	}
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("remote: create recipient: %w", err)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return nil, fmt.Errorf("remote: encrypt: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("remote: encrypt write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("remote: encrypt close: %w", err)
	}
	return buf.Bytes(), nil
}

// Decrypt decrypts age-encrypted data using the configured passphrase.
func Decrypt(data []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return data, nil
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("remote: create identity: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(data), id)
	if err != nil {
		return nil, fmt.Errorf("remote: decrypt: %w", err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("remote: decrypt read: %w", err)
	}
	return plain, nil
}

// Upload encrypts and uploads data to the remote destination.
func (c *Client) Upload(ctx context.Context, name string, data []byte) error {
	encrypted, err := Encrypt(data, c.cfg.EncryptKey)
	if err != nil {
		return err
	}

	switch c.cfg.Provider {
	case ProviderS3:
		return c.s3Put(ctx, name, encrypted)
	case ProviderHTTP:
		return c.httpPut(ctx, name, encrypted)
	default:
		return fmt.Errorf("remote: unsupported provider %q", c.cfg.Provider)
	}
}

// Download fetches and decrypts data from the remote destination.
func (c *Client) Download(ctx context.Context, name string) ([]byte, error) {
	var data []byte
	var err error

	switch c.cfg.Provider {
	case ProviderS3:
		data, err = c.s3Get(ctx, name)
	case ProviderHTTP:
		data, err = c.httpGet(ctx, name)
	default:
		return nil, fmt.Errorf("remote: unsupported provider %q", c.cfg.Provider)
	}
	if err != nil {
		return nil, err
	}

	return Decrypt(data, c.cfg.EncryptKey)
}

// List lists files at the remote destination with the given prefix.
func (c *Client) List(ctx context.Context, prefix string) ([]RemoteFile, error) {
	switch c.cfg.Provider {
	case ProviderS3:
		return c.s3List(ctx, prefix)
	default:
		return nil, fmt.Errorf("remote: list not supported for provider %q", c.cfg.Provider)
	}
}

// s3Put uploads to an S3-compatible endpoint using AWS Signature V4.
func (c *Client) s3Put(ctx context.Context, key string, data []byte) error {
	url := c.s3URL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("remote: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	c.signS3(req, data)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote: upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("remote: upload returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) s3Get(ctx context.Context, key string) ([]byte, error) {
	url := c.s3URL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("remote: create request: %w", err)
	}
	c.signS3(req, nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("remote: download returned %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

// s3ListResult is the XML response from S3 ListObjectsV2.
type s3ListResult struct {
	XMLName  xml.Name   `xml:"ListBucketResult"`
	Contents []s3Object `xml:"Contents"`
}

type s3Object struct {
	Key          string `xml:"Key"`
	Size         int64  `xml:"Size"`
	LastModified string `xml:"LastModified"`
}

func (c *Client) s3List(ctx context.Context, prefix string) ([]RemoteFile, error) {
	endpoint := strings.TrimRight(c.cfg.Endpoint, "/")
	url := fmt.Sprintf("%s/%s?list-type=2&prefix=%s", endpoint, c.cfg.Bucket, prefix)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("remote: create list request: %w", err)
	}
	c.signS3(req, nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("remote: list returned %d: %s", resp.StatusCode, string(body))
	}

	var result s3ListResult
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("remote: parse list response: %w", err)
	}

	files := make([]RemoteFile, 0, len(result.Contents))
	for _, obj := range result.Contents {
		modified, _ := time.Parse(time.RFC3339, obj.LastModified)
		files = append(files, RemoteFile{
			Name:     obj.Key,
			Size:     obj.Size,
			Modified: modified,
		})
	}
	return files, nil
}

func (c *Client) s3URL(key string) string {
	endpoint := strings.TrimRight(c.cfg.Endpoint, "/")
	return fmt.Sprintf("%s/%s/%s", endpoint, c.cfg.Bucket, key)
}

// signS3 applies a simplified AWS Signature V4-like signing.
// This is sufficient for S3-compatible endpoints like MinIO/B2.
func (c *Client) signS3(req *http.Request, payload []byte) {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	region := c.cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	payloadHash := sha256Hex(payload)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	// Canonical headers (sorted).
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
		req.Host, payloadHash, amzDate)

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveKey(c.cfg.SecretKey, dateStamp, region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKey, scope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func deriveKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// httpPut performs a simple HTTP PUT.
func (c *Client) httpPut(ctx context.Context, name string, data []byte) error {
	url := strings.TrimRight(c.cfg.Endpoint, "/") + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("remote: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote: upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("remote: upload returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) httpGet(ctx context.Context, name string) ([]byte, error) {
	url := strings.TrimRight(c.cfg.Endpoint, "/") + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("remote: create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("remote: download returned %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

// SortByModified sorts remote files by modification time, newest first.
func SortByModified(files []RemoteFile) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Modified.After(files[j].Modified)
	})
}
