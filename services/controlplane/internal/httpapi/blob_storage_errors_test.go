package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/logging"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

func TestFileStoreErrorLogsCauseAndPreservesProblemResponse(t *testing.T) {
	var logOutput bytes.Buffer
	logger, err := logging.NewWithWriter("info", &logOutput)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	handler := FileHandlers{Logger: logger}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/files/example.txt", nil)
	request = request.WithContext(ctxutil.WithRequestID(request.Context(), "req-blob-storage-123"))
	recorder := httptest.NewRecorder()
	cause := errors.New("dial tcp: lookup blob-storage.ace-infra.svc.cluster.local: no such host")

	handler.writeStoreError(recorder, request, cause)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"code":"blob_storage_unavailable"`) {
		t.Fatalf("problem response = %s, want blob_storage_unavailable", body)
	}
	if body := recorder.Body.String(); strings.Contains(body, cause.Error()) {
		t.Fatalf("problem response leaked storage cause: %s", body)
	}
	log := logOutput.String()
	if !strings.Contains(log, cause.Error()) {
		t.Fatalf("log did not contain storage cause: %s", log)
	}
	if !strings.Contains(log, "req-blob-storage-123") {
		t.Fatalf("log did not contain request ID: %s", log)
	}
}
