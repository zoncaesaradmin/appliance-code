package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"appliance-code/services/ordering/internal/order"
)

var headerIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{2,256}$`)

type Handler struct {
	Store     order.Store
	ChannelID string
	Ready     func(context.Context) error
}

func New(h Handler) (http.Handler, error) {
	if h.Store == nil || h.ChannelID == "" || !headerIDPattern.MatchString(h.ChannelID) {
		return nil, errors.New("invalid ordering HTTP handler")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		status(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if h.Ready != nil {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if err := h.Ready(ctx); err != nil {
				status(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
				return
			}
		}
		status(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/api/ordering/v1/release", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		status(w, http.StatusOK, map[string]string{"service": "ordering", "public_api_version": "v1"})
	})
	mux.HandleFunc("/api/ordering/v1/offerings", h.offerings)
	mux.HandleFunc("/api/ordering/v1/orders", h.orders)
	mux.HandleFunc("/api/ordering/v1/orders/", h.order)
	return mux, nil
}

func userID(r *http.Request) (string, error) {
	id := strings.TrimSpace(r.Header.Get("X-Appliance-User-Id"))
	if !headerIDPattern.MatchString(id) {
		return "", errors.New("authenticated user header is missing")
	}
	return id, nil
}

func (h Handler) offerings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	zoneID := strings.TrimSpace(r.URL.Query().Get("micro_zone_id"))
	if zoneID == "" {
		status(w, http.StatusBadRequest, map[string]string{"error": "micro_zone_id is required"})
		return
	}
	items, err := h.Store.ListOfferings(r.Context(), zoneID, 100)
	if err != nil {
		status(w, http.StatusServiceUnavailable, map[string]string{"error": "offerings unavailable"})
		return
	}
	status(w, http.StatusOK, map[string]any{"offerings": items})
}

func (h Handler) orders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := userID(r)
	if err != nil {
		status(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
		return
	}
	var input struct {
		MicroZoneID string            `json:"micro_zone_id"`
		Lines       []order.LineInput `json:"lines"`
	}
	if err := decodeJSON(w, r, &input, 32<<10); err != nil {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		status(w, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key is required"})
		return
	}
	created, err := h.Store.CreateOrder(r.Context(), id, order.CreateInput{MicroZoneID: input.MicroZoneID, ChannelID: interactionHeader(r, "X-Appliance-Channel-Id", h.ChannelID), ConversationID: interactionHeader(r, "X-Appliance-Conversation-Id", ""), CorrelationID: interactionHeader(r, "X-Appliance-Correlation-Id", newCorrelation()), IdempotencyKey: key, Lines: input.Lines})
	if err != nil {
		switch {
		case errors.Is(err, order.ErrConflict):
			status(w, http.StatusConflict, map[string]string{"error": "idempotency key was reused with different content"})
		case errors.Is(err, order.ErrNotFound):
			status(w, http.StatusNotFound, map[string]string{"error": "offering not found"})
		default:
			status(w, http.StatusServiceUnavailable, map[string]string{"error": "order unavailable"})
		}
		return
	}
	status(w, http.StatusCreated, created)
}

func (h Handler) order(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := userID(r)
	if err != nil {
		status(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
		return
	}
	orderID := strings.TrimPrefix(r.URL.Path, "/api/ordering/v1/orders/")
	if orderID == "" || strings.Contains(orderID, "/") {
		status(w, http.StatusNotFound, map[string]string{"error": "order not found"})
		return
	}
	item, err := h.Store.GetOrder(r.Context(), id, orderID)
	if errors.Is(err, order.ErrNotFound) {
		status(w, http.StatusNotFound, map[string]string{"error": "order not found"})
		return
	}
	if err != nil {
		status(w, http.StatusServiceUnavailable, map[string]string{"error": "order unavailable"})
		return
	}
	status(w, http.StatusOK, item)
}

func interactionHeader(r *http.Request, name, fallback string) string {
	value := strings.TrimSpace(r.Header.Get(name))
	if value == "" || !headerIDPattern.MatchString(value) {
		return fallback
	}
	return value
}
func newCorrelation() string { return time.Now().UTC().Format("20060102T150405.000000000Z") }
func decodeJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) error {
	if r.Body == nil {
		status(w, http.StatusBadRequest, map[string]string{"error": "JSON body is required"})
		return errors.New("missing body")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		status(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		status(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return errors.New("trailing JSON")
	}
	return nil
}
func status(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
