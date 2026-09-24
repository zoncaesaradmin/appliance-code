// Package chat implements the native, appliance-owned chat experience. It
// owns transcript persistence and turns, while inference remains responsible
// for runtime-specific transport.
package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/inference"
	"appliance-code/services/controlplane/internal/storage"
)

const (
	maxConversations = 100
	maxPromptBytes   = 16 << 10
	maxReplyBytes    = 64 << 10
	maxHistoryBytes  = 64 << 10
)

var (
	ErrUnavailable   = errors.New("chat: no model is ready")
	ErrModelChanged  = errors.New("chat: this conversation belongs to a model that is no longer enabled")
	ErrTooMany       = errors.New("chat: conversation limit reached")
	ErrInvalidPrompt = errors.New("chat: prompt must be non-empty and within the size limit")
)

type Availability struct {
	Ready       bool   `json:"ready"`
	ModelID     string `json:"modelId,omitempty"`
	Reason      string `json:"reason,omitempty"`
	MaxModelLen uint64 `json:"maxModelLen,omitempty"`
}
type Service struct {
	store     storage.ChatStore
	inference *inference.Service
}

func New(store storage.ChatStore, inferenceService *inference.Service) (*Service, error) {
	if store == nil || inferenceService == nil {
		return nil, errors.New("chat: store and inference service are required")
	}
	return &Service{store: store, inference: inferenceService}, nil
}

func (s *Service) Availability(ctx context.Context) Availability {
	statusCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	progress, err := s.inference.LoadProgress(statusCtx)
	if err != nil || progress.State != "ready" || strings.TrimSpace(progress.ModelID) == "" {
		return Availability{Reason: "No model is enabled. Ask an administrator to enable a downloaded model."}
	}
	return Availability{Ready: true, ModelID: progress.ModelID}
}

func (s *Service) CreateConversation(ctx context.Context, ownerID string) (storage.ChatConversation, error) {
	if len(ownerID) == 0 {
		return storage.ChatConversation{}, errors.New("chat: owner is required")
	}
	items, err := s.store.ListConversations(ctx, ownerID, maxConversations+1)
	if err != nil {
		return storage.ChatConversation{}, err
	}
	if len(items) >= maxConversations {
		return storage.ChatConversation{}, ErrTooMany
	}
	availability := s.Availability(ctx)
	if !availability.Ready {
		return storage.ChatConversation{}, ErrUnavailable
	}
	now := time.Now().UTC()
	c := storage.ChatConversation{ID: uuid.Must(uuid.NewV7()).String(), OwnerID: ownerID, ModelID: availability.ModelID, Title: "New conversation", CreatedAt: now, UpdatedAt: now}
	return c, s.store.CreateConversation(ctx, c)
}

func (s *Service) List(ctx context.Context, ownerID string) ([]storage.ChatConversation, error) {
	return s.store.ListConversations(ctx, ownerID, maxConversations)
}
func (s *Service) Get(ctx context.Context, ownerID, id string) (storage.ChatConversation, []storage.ChatMessage, error) {
	return s.store.GetConversation(ctx, ownerID, id)
}
func (s *Service) Delete(ctx context.Context, ownerID, id string) error {
	return s.store.DeleteConversation(ctx, ownerID, id)
}

// StreamTurn writes assistant deltas through emit. The user prompt is made
// durable before inference starts; interrupted replies remain visible as such
// after a pod restart rather than being silently lost.
func (s *Service) StreamTurn(ctx context.Context, ownerID, conversationID, content string, emit func(event string, value any) error) error {
	content = strings.TrimSpace(content)
	if content == "" || len(content) > maxPromptBytes {
		return ErrInvalidPrompt
	}
	c, history, err := s.store.GetConversation(ctx, ownerID, conversationID)
	if err != nil {
		return err
	}
	availability := s.Availability(ctx)
	if !availability.Ready {
		return ErrUnavailable
	}
	if availability.ModelID != c.ModelID {
		return ErrModelChanged
	}
	now := time.Now().UTC()
	sequence := len(history)
	user := storage.ChatMessage{ID: uuid.Must(uuid.NewV7()).String(), ConversationID: c.ID, Role: "user", Content: content, Status: "complete", Sequence: sequence, CreatedAt: now, UpdatedAt: now}
	if err := s.store.AppendMessage(ctx, user); err != nil {
		return err
	}
	if c.Title == "New conversation" {
		title := strings.Join(strings.Fields(content), " ")
		if len(title) > 80 {
			title = title[:80] + "…"
		}
		_ = s.store.UpdateConversationTitle(ctx, ownerID, c.ID, title)
	}
	assistant := storage.ChatMessage{ID: uuid.Must(uuid.NewV7()).String(), ConversationID: c.ID, Role: "assistant", Status: "streaming", Sequence: sequence + 1, CreatedAt: now, UpdatedAt: now}
	if err := s.store.AppendMessage(ctx, assistant); err != nil {
		return err
	}
	if err := emit("started", map[string]string{"messageId": assistant.ID}); err != nil {
		return err
	}

	messages := make([]inference.ChatMessage, 0, len(history)+1)
	used := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Status != "complete" {
			continue
		}
		if used+len(history[i].Content) > maxHistoryBytes {
			break
		}
		used += len(history[i].Content)
		messages = append(messages, inference.ChatMessage{Role: history[i].Role, Content: history[i].Content})
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	messages = append(messages, inference.ChatMessage{Role: "user", Content: content})
	response, err := s.inference.StreamChat(ctx, inference.ChatRequest{Model: c.ModelID, Messages: messages, Stream: true})
	if err != nil {
		_ = s.store.UpdateMessage(context.Background(), ownerID, assistant.ID, "", "failed")
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		_ = s.store.UpdateMessage(context.Background(), ownerID, assistant.ID, "", "failed")
		return fmt.Errorf("chat: inference failed: %s", strings.TrimSpace(string(raw)))
	}
	text := ""
	streamErr := consumeSSE(response.Body, func(delta string) error {
		if len(text)+len(delta) > maxReplyBytes {
			return errors.New("chat: reply exceeded storage limit")
		}
		text += delta
		if err := s.store.UpdateMessage(ctx, ownerID, assistant.ID, text, "streaming"); err != nil {
			return err
		}
		return emit("delta", map[string]string{"messageId": assistant.ID, "content": delta})
	})
	status := "complete"
	event := "completed"
	if streamErr != nil {
		status, event = "failed", "failed"
		if errors.Is(streamErr, context.Canceled) {
			status = "stopped"
		}
	}
	_ = s.store.UpdateMessage(context.Background(), ownerID, assistant.ID, text, status)
	if streamErr != nil {
		return streamErr
	}
	return emit(event, map[string]string{"messageId": assistant.ID})
}

func consumeSSE(body io.Reader, onDelta func(string) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := onDelta(choice.Delta.Content); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}
