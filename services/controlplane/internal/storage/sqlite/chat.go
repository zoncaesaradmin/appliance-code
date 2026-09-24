package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

type chatStore struct{ db *DB }

func NewChatStore(db *DB) storage.ChatStore { return &chatStore{db: db} }

func (s *chatStore) CreateConversation(ctx context.Context, c storage.ChatConversation) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `INSERT INTO chat_conversations(id, owner_id, model_id, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, c.ID, c.OwnerID, c.ModelID, c.Title, stamp(c.CreatedAt), stamp(c.UpdatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: creating chat conversation: %w", err)
	}
	return nil
}

func (s *chatStore) ListConversations(ctx context.Context, ownerID string, limit int) ([]storage.ChatConversation, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT id, owner_id, model_id, title, created_at, updated_at FROM chat_conversations WHERE owner_id=? ORDER BY updated_at DESC LIMIT ?`, ownerID, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing chat conversations: %w", err)
	}
	defer rows.Close()
	items := []storage.ChatConversation{}
	for rows.Next() {
		var c storage.ChatConversation
		var created, updated string
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.ModelID, &c.Title, &created, &updated); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		items = append(items, c)
	}
	return items, rows.Err()
}

func (s *chatStore) GetConversation(ctx context.Context, ownerID, id string) (storage.ChatConversation, []storage.ChatMessage, error) {
	var c storage.ChatConversation
	var created, updated string
	err := s.db.q(ctx).QueryRowContext(ctx, `SELECT id, owner_id, model_id, title, created_at, updated_at FROM chat_conversations WHERE id=? AND owner_id=?`, id, ownerID).Scan(&c.ID, &c.OwnerID, &c.ModelID, &c.Title, &created, &updated)
	if err == sql.ErrNoRows {
		return storage.ChatConversation{}, nil, storage.ErrNotFound
	}
	if err != nil {
		return storage.ChatConversation{}, nil, fmt.Errorf("sqlite: getting chat conversation: %w", err)
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT id, conversation_id, role, content, status, sequence, created_at, updated_at FROM chat_messages WHERE conversation_id=? ORDER BY sequence ASC`, id)
	if err != nil {
		return storage.ChatConversation{}, nil, fmt.Errorf("sqlite: listing chat messages: %w", err)
	}
	defer rows.Close()
	messages := []storage.ChatMessage{}
	for rows.Next() {
		var m storage.ChatMessage
		var mc, mu string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Status, &m.Sequence, &mc, &mu); err != nil {
			return storage.ChatConversation{}, nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, mc)
		m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, mu)
		messages = append(messages, m)
	}
	return c, messages, rows.Err()
}

func (s *chatStore) DeleteConversation(ctx context.Context, ownerID, id string) error {
	result, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM chat_conversations WHERE id=? AND owner_id=?`, id, ownerID)
	if err != nil {
		return fmt.Errorf("sqlite: deleting chat conversation: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *chatStore) UpdateConversationTitle(ctx context.Context, ownerID, id, title string) error {
	result, err := s.db.q(ctx).ExecContext(ctx, `UPDATE chat_conversations SET title=?, updated_at=? WHERE id=? AND owner_id=?`, title, stamp(time.Now().UTC()), id, ownerID)
	if err != nil {
		return fmt.Errorf("sqlite: updating chat conversation title: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *chatStore) AppendMessage(ctx context.Context, m storage.ChatMessage) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `INSERT INTO chat_messages(id, conversation_id, role, content, status, sequence, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.ID, m.ConversationID, m.Role, m.Content, m.Status, m.Sequence, stamp(m.CreatedAt), stamp(m.UpdatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: appending chat message: %w", err)
	}
	_, err = s.db.q(ctx).ExecContext(ctx, `UPDATE chat_conversations SET updated_at=? WHERE id=?`, stamp(m.UpdatedAt), m.ConversationID)
	return err
}

func (s *chatStore) UpdateMessage(ctx context.Context, ownerID, messageID, content, status string) error {
	result, err := s.db.q(ctx).ExecContext(ctx, `UPDATE chat_messages SET content=?, status=?, updated_at=? WHERE id=? AND conversation_id IN (SELECT id FROM chat_conversations WHERE owner_id=?)`, content, status, stamp(time.Now().UTC()), messageID, ownerID)
	if err != nil {
		return fmt.Errorf("sqlite: updating chat message: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
