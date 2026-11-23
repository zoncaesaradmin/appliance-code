package order

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrConflict    = errors.New("idempotency key conflicts with a previous request")
	ErrNotFound    = errors.New("order not found")
	ErrUnavailable = errors.New("ordering store unavailable")
	idPattern      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	zonePattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{2,128}$`)
	keyPattern     = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
	contextPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{2,256}$`)
)

type LineInput struct {
	OfferingID string `json:"offering_id"`
	Quantity   int    `json:"quantity"`
}

type CreateInput struct {
	MicroZoneID    string
	ChannelID      string
	ConversationID string
	CorrelationID  string
	IdempotencyKey string
	Lines          []LineInput
}

type Offering struct {
	ID          string    `json:"id"`
	MerchantID  string    `json:"merchant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceMinor  int64     `json:"price_minor"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
}

type OrderLine struct {
	LineNumber     int    `json:"line_number"`
	OfferingID     string `json:"offering_id"`
	OfferingName   string `json:"offering_name"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
	Quantity       int    `json:"quantity"`
	LineTotalMinor int64  `json:"line_total_minor"`
}

type Order struct {
	ID             string      `json:"id"`
	UserID         string      `json:"user_id"`
	MerchantID     string      `json:"merchant_id"`
	MicroZoneID    string      `json:"micro_zone_id"`
	ChannelID      string      `json:"channel_id"`
	ConversationID string      `json:"conversation_id,omitempty"`
	CorrelationID  string      `json:"correlation_id"`
	Status         string      `json:"status"`
	Currency       string      `json:"currency"`
	TotalMinor     int64       `json:"total_minor"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Lines          []OrderLine `json:"lines"`
}

type Store interface {
	ListOfferings(context.Context, string, int) ([]Offering, error)
	CreateOrder(context.Context, string, CreateInput) (Order, error)
	GetOrder(context.Context, string, string) (Order, error)
}

type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("missing ordering database")
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) ListOfferings(ctx context.Context, zoneID string, limit int) ([]Offering, error) {
	if !zonePattern.MatchString(zoneID) || limit < 1 || limit > 100 {
		return nil, errors.New("invalid offering query")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.merchant_id, o.name, o.description, o.price_minor, o.currency, o.created_at
		FROM ordering_offerings o
		JOIN ordering_merchants m ON m.id = o.merchant_id AND m.active
		JOIN ordering_offering_micro_zones z ON z.offering_id = o.id AND z.micro_zone_id = $1
		WHERE o.active ORDER BY o.created_at, o.id LIMIT $2`, zoneID, limit)
	if err != nil {
		return nil, fmt.Errorf("list offerings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]Offering, 0, limit)
	for rows.Next() {
		var item Offering
		if err := rows.Scan(&item.ID, &item.MerchantID, &item.Name, &item.Description, &item.PriceMinor, &item.Currency, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan offering: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read offerings: %w", err)
	}
	return items, nil
}

func (s *PostgresStore) CreateOrder(ctx context.Context, userID string, input CreateInput) (Order, error) {
	if !idPattern.MatchString(userID) || !zonePattern.MatchString(input.MicroZoneID) || !contextPattern.MatchString(input.ChannelID) || (input.ConversationID != "" && !contextPattern.MatchString(input.ConversationID)) || !contextPattern.MatchString(input.CorrelationID) || !keyPattern.MatchString(input.IdempotencyKey) || len(input.Lines) == 0 || len(input.Lines) > 50 {
		return Order{}, errors.New("invalid order")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return Order{}, errors.New("invalid order")
	}
	hash := sha256.Sum256(append([]byte("ordering-create-v1\x00"), body...))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, fmt.Errorf("begin order: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO ordering_idempotency_keys(user_id,request_key,request_hash) VALUES($1,$2,$3) ON CONFLICT(user_id,request_key) DO NOTHING`, userID, input.IdempotencyKey, hash[:])
	if err != nil {
		return Order{}, fmt.Errorf("reserve order: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return Order{}, fmt.Errorf("check order reservation: %w", err)
	}
	if inserted == 0 {
		var previous []byte
		var orderID sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT request_hash,order_id FROM ordering_idempotency_keys WHERE user_id=$1 AND request_key=$2`, userID, input.IdempotencyKey).Scan(&previous, &orderID); err != nil {
			return Order{}, fmt.Errorf("read order reservation: %w", err)
		}
		if !equalBytes(hash[:], previous) {
			return Order{}, ErrConflict
		}
		if !orderID.Valid {
			return Order{}, ErrUnavailable
		}
		return s.getOrderTx(ctx, tx, userID, orderID.String)
	}

	if len(input.Lines) == 0 {
		return Order{}, errors.New("order needs at least one line")
	}
	orderID, err := newID()
	if err != nil {
		return Order{}, err
	}
	order := Order{ID: orderID, UserID: userID, MicroZoneID: input.MicroZoneID, ChannelID: input.ChannelID, ConversationID: input.ConversationID, CorrelationID: input.CorrelationID, Status: "submitted"}
	var merchantID, currency string
	for i, line := range input.Lines {
		if !idPattern.MatchString(line.OfferingID) || line.Quantity < 1 || line.Quantity > 1000 {
			return Order{}, errors.New("invalid order line")
		}
		var item Offering
		err := tx.QueryRowContext(ctx, `
			SELECT o.id,o.merchant_id,o.name,o.price_minor,o.currency
			FROM ordering_offerings o
			JOIN ordering_merchants m ON m.id=o.merchant_id AND m.active
			JOIN ordering_offering_micro_zones z ON z.offering_id=o.id AND z.micro_zone_id=$2
			WHERE o.id=$1 AND o.active`, line.OfferingID, input.MicroZoneID).Scan(&item.ID, &item.MerchantID, &item.Name, &item.PriceMinor, &item.Currency)
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, ErrNotFound
		}
		if err != nil {
			return Order{}, fmt.Errorf("read offering: %w", err)
		}
		if i == 0 {
			merchantID, currency = item.MerchantID, item.Currency
		} else if merchantID != item.MerchantID || currency != item.Currency {
			return Order{}, errors.New("all order lines must belong to one merchant and currency")
		}
		lineTotal, ok := safeMultiply(item.PriceMinor, int64(line.Quantity))
		if !ok {
			return Order{}, errors.New("order total is too large")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ordering_order_lines(order_id,line_number,offering_id,offering_name,unit_price_minor,quantity,line_total_minor) VALUES($1,$2,$3,$4,$5,$6,$7)`, orderID, i+1, item.ID, item.Name, item.PriceMinor, line.Quantity, lineTotal); err != nil {
			return Order{}, fmt.Errorf("insert order line: %w", err)
		}
		order.TotalMinor, ok = safeAdd(order.TotalMinor, lineTotal)
		if !ok {
			return Order{}, errors.New("order total is too large")
		}
	}
	order.MerchantID, order.Currency = merchantID, currency
	if err := tx.QueryRowContext(ctx, `INSERT INTO ordering_orders(id,user_id,merchant_id,micro_zone_id,channel_id,conversation_id,correlation_id,status,currency,total_minor) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10) RETURNING created_at,updated_at`, order.ID, order.UserID, order.MerchantID, order.MicroZoneID, order.ChannelID, order.ConversationID, order.CorrelationID, order.Status, order.Currency, order.TotalMinor).Scan(&order.CreatedAt, &order.UpdatedAt); err != nil {
		return Order{}, fmt.Errorf("insert order: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ordering_idempotency_keys SET order_id=$3 WHERE user_id=$1 AND request_key=$2`, userID, input.IdempotencyKey, order.ID); err != nil {
		return Order{}, fmt.Errorf("complete order reservation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("commit order: %w", err)
	}
	order.CreatedAt, order.UpdatedAt = order.CreatedAt.UTC(), order.UpdatedAt.UTC()
	return s.getOrder(ctx, userID, order.ID)
}

func (s *PostgresStore) GetOrder(ctx context.Context, userID, orderID string) (Order, error) {
	if !idPattern.MatchString(userID) || !idPattern.MatchString(orderID) {
		return Order{}, errors.New("invalid order lookup")
	}
	return s.getOrder(ctx, userID, orderID)
}

func (s *PostgresStore) getOrder(ctx context.Context, userID, orderID string) (Order, error) {
	return s.getOrderTx(ctx, nil, userID, orderID)
}

type queryRow interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *PostgresStore) getOrderTx(ctx context.Context, tx *sql.Tx, userID, orderID string) (Order, error) {
	var row queryRow = s.db
	if tx != nil {
		row = tx
	}
	var order Order
	if err := row.QueryRowContext(ctx, `SELECT id,user_id,merchant_id,micro_zone_id,channel_id,COALESCE(conversation_id,''),correlation_id,status,currency,total_minor,created_at,updated_at FROM ordering_orders WHERE id=$1 AND user_id=$2`, orderID, userID).Scan(&order.ID, &order.UserID, &order.MerchantID, &order.MicroZoneID, &order.ChannelID, &order.ConversationID, &order.CorrelationID, &order.Status, &order.Currency, &order.TotalMinor, &order.CreatedAt, &order.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	} else if err != nil {
		return Order{}, fmt.Errorf("read order: %w", err)
	}
	rows, err := queryRowsFrom(ctx, row, `SELECT line_number,offering_id,offering_name,unit_price_minor,quantity,line_total_minor FROM ordering_order_lines WHERE order_id=$1 ORDER BY line_number`, order.ID)
	if err != nil {
		return Order{}, fmt.Errorf("read order lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var line OrderLine
		if err := rows.Scan(&line.LineNumber, &line.OfferingID, &line.OfferingName, &line.UnitPriceMinor, &line.Quantity, &line.LineTotalMinor); err != nil {
			return Order{}, fmt.Errorf("scan order line: %w", err)
		}
		order.Lines = append(order.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return Order{}, fmt.Errorf("read order lines: %w", err)
	}
	order.CreatedAt, order.UpdatedAt = order.CreatedAt.UTC(), order.UpdatedAt.UTC()
	return order, nil
}

type queryRows interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryRowsFrom(ctx context.Context, q queryRows, query string, args ...any) (*sql.Rows, error) {
	return q.QueryContext(ctx, query, args...)
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("cannot generate order identifier")
	}
	return hex.EncodeToString(b[:]), nil
}
func equalBytes(a, b []byte) bool {
	return bytes.Equal(a, b)
}
func safeMultiply(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || (a != 0 && b > int64(^uint64(0)>>1)/a) {
		return 0, false
	}
	return a * b, true
}
func safeAdd(a, b int64) (int64, bool) {
	if b > 0 && a > int64(^uint64(0)>>1)-b {
		return 0, false
	}
	return a + b, true
}
