package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

func TestNativeChatWireShapeUsesCamelCase(t *testing.T) {
	conversation := storage.ChatConversation{ID: "conversation-1", OwnerID: "user-1", ModelID: "qwen3:8b", Title: "Hello", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	message := storage.ChatMessage{ID: "message-1", ConversationID: conversation.ID, Role: "user", Content: "Hello", Status: "complete", Sequence: 0, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	raw, err := json.Marshal(map[string]any{"conversation": conversation, "messages": []storage.ChatMessage{message}})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Conversation struct {
			ID      string `json:"id"`
			ModelID string `json:"modelId"`
		} `json:"conversation"`
		Messages []struct {
			ConversationID string `json:"conversationId"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Conversation.ID != conversation.ID || got.Conversation.ModelID != conversation.ModelID || len(got.Messages) != 1 || got.Messages[0].ConversationID != conversation.ID {
		t.Fatalf("native chat response has wrong shape: %s", raw)
	}
	if _, present := body["Conversation"]; present {
		t.Fatalf("wire response leaked Go-style field name: %s", raw)
	}
}
