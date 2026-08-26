// Package blobstore provides a small, dependency-free S3 client for appliance
// payloads.  The same contract works with the local S3 service and cloud S3.
package blobstore

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
	"path"
	"sort"
	"strings"
	"time"
)

type Object struct {
	Key         string
	Size        int64
	ModifiedAt  time.Time
	ContentType string
	ETag        string
}

type ListResult struct {
	Objects        []Object
	CommonPrefixes []string
}

type Client struct {
	Endpoint  *url.URL
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	HTTP      *http.Client
	Now       func() time.Time
}

func New(endpoint, bucket, accessKey, secretKey, region string) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Scheme == "" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("blob storage endpoint must be an absolute http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("blob storage endpoint must use http or https")
	}
	if strings.TrimSpace(bucket) == "" || strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("blob storage bucket and credentials are required")
	}
	return &Client{Endpoint: u, Bucket: bucket, AccessKey: accessKey, SecretKey: secretKey, Region: defaultRegion(region), HTTP: http.DefaultClient, Now: time.Now}, nil
}

func (c *Client) EnsureBucket(ctx context.Context) error {
	request, err := c.newRequest(ctx, http.MethodPut, "", nil, 0, "")
	if err != nil {
		return err
	}
	response, err := c.do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusConflict {
		return nil
	}
	return responseError(response)
}

func (c *Client) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) (Object, error) {
	request, err := c.newRequest(ctx, http.MethodPut, key, body, size, contentType)
	if err != nil {
		return Object{}, err
	}
	response, err := c.do(request)
	if err != nil {
		return Object{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return Object{}, responseError(response)
	}
	return Object{Key: key, Size: size, ContentType: contentType, ETag: strings.Trim(response.Header.Get("ETag"), "\"")}, nil
}

func (c *Client) Get(ctx context.Context, key, byteRange string) (*http.Response, Object, error) {
	request, err := c.newRequest(ctx, http.MethodGet, key, nil, 0, "")
	if err != nil {
		return nil, Object{}, err
	}
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response, err := c.do(request)
	if err != nil {
		return nil, Object{}, err
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		defer response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			return nil, Object{}, ErrNotFound
		}
		return nil, Object{}, responseError(response)
	}
	return response, objectFromHeaders(key, response), nil
}

func (c *Client) Stat(ctx context.Context, key string) (Object, error) {
	request, err := c.newRequest(ctx, http.MethodHead, key, nil, 0, "")
	if err != nil {
		return Object{}, err
	}
	response, err := c.do(request)
	if err != nil {
		return Object{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return Object{}, ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		return Object{}, responseError(response)
	}
	return objectFromHeaders(key, response), nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	request, err := c.newRequest(ctx, http.MethodDelete, key, nil, 0, "")
	if err != nil {
		return err
	}
	response, err := c.do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNotFound {
		return nil
	}
	return responseError(response)
}

func (c *Client) List(ctx context.Context, prefix string) (ListResult, error) {
	return c.list(ctx, prefix, true)
}

// ListAll returns every object below prefix. It is used only for deliberate
// recursive deletes; normal directory rendering uses List with a delimiter.
func (c *Client) ListAll(ctx context.Context, prefix string) (ListResult, error) {
	return c.list(ctx, prefix, false)
}

func (c *Client) list(ctx context.Context, prefix string, delimiter bool) (ListResult, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "", nil, 0, "")
	if err != nil {
		return ListResult{}, err
	}
	query := request.URL.Query()
	query.Set("list-type", "2")
	if delimiter {
		query.Set("delimiter", "/")
	}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	request.URL.RawQuery = query.Encode()
	c.sign(request, "UNSIGNED-PAYLOAD")
	response, err := c.do(request)
	if err != nil {
		return ListResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ListResult{}, responseError(response)
	}
	var document struct {
		Contents []struct {
			Key          string    `xml:"Key"`
			LastModified time.Time `xml:"LastModified"`
			Size         int64     `xml:"Size"`
			ETag         string    `xml:"ETag"`
		} `xml:"Contents"`
		Prefixes []struct {
			Prefix string `xml:"Prefix"`
		} `xml:"CommonPrefixes"`
	}
	if err := xml.NewDecoder(response.Body).Decode(&document); err != nil {
		return ListResult{}, fmt.Errorf("decode S3 list response: %w", err)
	}
	result := ListResult{Objects: make([]Object, 0, len(document.Contents)), CommonPrefixes: make([]string, 0, len(document.Prefixes))}
	for _, item := range document.Contents {
		result.Objects = append(result.Objects, Object{Key: item.Key, Size: item.Size, ModifiedAt: item.LastModified, ETag: strings.Trim(item.ETag, "\"")})
	}
	for _, item := range document.Prefixes {
		result.CommonPrefixes = append(result.CommonPrefixes, item.Prefix)
	}
	sort.Slice(result.Objects, func(i, j int) bool { return result.Objects[i].Key < result.Objects[j].Key })
	sort.Strings(result.CommonPrefixes)
	return result, nil
}

var ErrNotFound = fmt.Errorf("blob not found")

func (c *Client) newRequest(ctx context.Context, method, key string, body io.Reader, size int64, contentType string) (*http.Request, error) {
	u := *c.Endpoint
	segments := []string{strings.Trim(u.Path, "/"), strings.Trim(c.Bucket, "/"), strings.Trim(key, "/")}
	u.Path = "/" + strings.Trim(path.Join(segments...), "/")
	if key == "" {
		u.Path = "/" + strings.Trim(path.Join(segments[:2]...), "/")
	}
	request, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if size > 0 {
		request.ContentLength = size
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	c.sign(request, "UNSIGNED-PAYLOAD")
	return request, nil
}

func (c *Client) do(request *http.Request) (*http.Response, error) {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(request)
}

func (c *Client) sign(request *http.Request, payloadHash string) {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	date := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	request.Header.Set("Host", request.URL.Host)
	request.Header.Set("X-Amz-Date", date)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	canonicalHeaders := "host:" + request.URL.Host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + date + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{request.Method, request.URL.EscapedPath(), request.URL.Query().Encode(), canonicalHeaders, signedHeaders, payloadHash}, "\n")
	scope := day + "/" + c.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + date + "\n" + scope + "\n" + sha256Hex(canonicalRequest)
	signingKey := hmacBytes(hmacBytes(hmacBytes(hmacBytes([]byte("AWS4"+c.SecretKey), day), c.Region), "s3"), "aws4_request")
	signature := hex.EncodeToString(hmacBytes(signingKey, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.AccessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func objectFromHeaders(key string, response *http.Response) Object {
	size := response.ContentLength
	if value := response.Header.Get("Content-Length"); value != "" {
		fmt.Sscan(value, &size)
	}
	modified, _ := http.ParseTime(response.Header.Get("Last-Modified"))
	return Object{Key: key, Size: size, ModifiedAt: modified, ContentType: response.Header.Get("Content-Type"), ETag: strings.Trim(response.Header.Get("ETag"), "\"")}
}
func responseError(response *http.Response) error {
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
	return fmt.Errorf("S3 request failed: %s: %s", response.Status, strings.TrimSpace(string(bytes.TrimSpace(payload))))
}
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func hmacBytes(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}
func defaultRegion(region string) string {
	if strings.TrimSpace(region) == "" {
		return "us-east-1"
	}
	return strings.TrimSpace(region)
}
