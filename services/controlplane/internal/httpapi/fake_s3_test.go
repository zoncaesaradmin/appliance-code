package httpapi_test

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 exercises the control plane against the S3 HTTP protocol rather
// than coupling video tests to a local backing directory.
type fakeS3 struct {
	*httptest.Server
	mu      sync.Mutex
	objects map[string]fakeS3Object
}

type fakeS3Object struct {
	data     []byte
	modified time.Time
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()
	store := &fakeS3{objects: map[string]fakeS3Object{}}
	store.Server = httptest.NewServer(http.HandlerFunc(store.serveHTTP))
	t.Cleanup(store.Close)
	return store
}

func (s *fakeS3) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[key]
	return ok
}

func (s *fakeS3) serveHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if r.URL.Query().Get("list-type") == "2" {
		s.list(w, r, key)
		return
	}
	if key == "appliance" && r.Method == http.MethodPut {
		w.WriteHeader(http.StatusOK)
		return
	}
	s.mu.Lock()
	object, exists := s.objects[key]
	s.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.objects[key] = fakeS3Object{data: body, modified: time.Now().UTC()}
		s.mu.Unlock()
		w.Header().Set("ETag", "\"test-etag\"")
		w.WriteHeader(http.StatusOK)
	case http.MethodHead:
		if !exists {
			http.NotFound(w, r)
			return
		}
		s.writeHeaders(w, object)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		if !exists {
			http.NotFound(w, r)
			return
		}
		s.writeHeaders(w, object)
		start, end, partial := rangeBounds(r.Header.Get("Range"), int64(len(object.data)))
		if partial {
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.Itoa(len(object.data)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(object.data[start : end+1])
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(object.data)
	case http.MethodDelete:
		if !exists {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		delete(s.objects, key)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *fakeS3) writeHeaders(w http.ResponseWriter, object fakeS3Object) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.Itoa(len(object.data)))
	w.Header().Set("Last-Modified", object.modified.Format(http.TimeFormat))
	w.Header().Set("ETag", "\"test-etag\"")
	w.Header().Set("Accept-Ranges", "bytes")
}

func (s *fakeS3) list(w http.ResponseWriter, r *http.Request, bucket string) {
	if bucket != "appliance" {
		http.NotFound(w, r)
		return
	}
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	type content struct {
		Key          string    `xml:"Key"`
		LastModified time.Time `xml:"LastModified"`
		Size         int64     `xml:"Size"`
		ETag         string    `xml:"ETag"`
	}
	type commonPrefix struct {
		Prefix string `xml:"Prefix"`
	}
	type response struct {
		XMLName  xml.Name       `xml:"ListBucketResult"`
		Contents []content      `xml:"Contents"`
		Prefixes []commonPrefix `xml:"CommonPrefixes"`
	}
	result := response{}
	directories := map[string]struct{}{}
	s.mu.Lock()
	for key, object := range s.objects {
		if !strings.HasPrefix(key, "appliance/"+prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, "appliance/"+prefix)
		if delimiter != "" && strings.Contains(rest, delimiter) {
			directories["appliance/"+prefix+strings.SplitN(rest, delimiter, 2)[0]+delimiter] = struct{}{}
			continue
		}
		result.Contents = append(result.Contents, content{Key: strings.TrimPrefix(key, "appliance/"), LastModified: object.modified, Size: int64(len(object.data)), ETag: "test-etag"})
	}
	s.mu.Unlock()
	for directory := range directories {
		result.Prefixes = append(result.Prefixes, commonPrefix{Prefix: strings.TrimPrefix(directory, "appliance/")})
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(result)
}

func rangeBounds(header string, total int64) (int64, int64, bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, total - 1, false
	}
	parts := strings.SplitN(strings.TrimPrefix(header, "bytes="), "-", 2)
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= total {
		return 0, total - 1, false
	}
	end := total - 1
	if len(parts) == 2 && parts[1] != "" {
		if parsed, err := strconv.ParseInt(parts[1], 10, 64); err == nil && parsed < end {
			end = parsed
		}
	}
	return start, end, true
}
