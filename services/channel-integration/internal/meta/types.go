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
	Statuses []Status  `json:"statuses"`
}
type Message struct {
	ID       string    `json:"id,omitempty"`
	From     string    `json:"from"`
	Type     string    `json:"type"`
	Text     *Text     `json:"text,omitempty"`
	Location *Location `json:"location,omitempty"`
}

// Status is a provider acknowledgement/update. Statuses are separate from
// inbound messages so they cannot trigger a customer reply.
type Status struct {
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
}
type Text struct {
	Body string `json:"body"`
}
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
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
type SendTemplateRequest struct {
	MessagingProduct string      `json:"messaging_product"`
	To               string      `json:"to"`
	Type             string      `json:"type"`
	Template         OTPTemplate `json:"template"`
}
type OTPTemplate struct {
	Name       string              `json:"name"`
	Language   TemplateLanguage    `json:"language"`
	Components []TemplateComponent `json:"components,omitempty"`
}
type TemplateLanguage struct {
	Code string `json:"code"`
}
type TemplateComponent struct {
	Type       string              `json:"type"`
	Parameters []TemplateParameter `json:"parameters,omitempty"`
}
type TemplateParameter struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type SendMessageResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}
