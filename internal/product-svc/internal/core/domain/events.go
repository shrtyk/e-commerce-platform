package domain

type DomainEvent struct {
	EventID       string
	EventName     string
	AggregateType string
	AggregateID   string
	Topic         string
	Key           string
	Payload       map[string]any
	Headers       map[string]string
}
