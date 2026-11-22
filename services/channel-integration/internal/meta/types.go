package meta

type WebhookPayload struct {
	Object string  `json:"object"`
	Entry  []Entry `json:"entry"`
}
type Entry struct {
	Changes []Change `json:"changes"`
}
type Change struct {
	Value Value `json:"value"`
}
type Value struct {
	Messages []Message `json:"messages"`
}
type Message struct {
	From string `json:"from"`
	Type string `json:"type"`
	Text *Text  `json:"text,omitempty"`
}
type Text struct {
	Body string `json:"body"`
}
type SendTextRequest struct {
	MessagingProduct string   `json:"messaging_product"`
	RecipientType    string   `json:"recipient_type"`
	To               string   `json:"to"`
	Type             string   `json:"type"`
	Text             SendText `json:"text"`
}
type SendText struct {
	PreviewURL bool   `json:"preview_url"`
	Body       string `json:"body"`
}
type SendMessageResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}
