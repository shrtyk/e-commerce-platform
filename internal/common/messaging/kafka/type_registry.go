package kafka

import (
	"fmt"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
)

type TypeRegistry struct {
	mu        sync.RWMutex
	factories map[string]func() proto.Message
}

func NewTypeRegistry() *TypeRegistry {
	return &TypeRegistry{factories: make(map[string]func() proto.Message)}
}

func (r *TypeRegistry) RegisterMessages(samples ...proto.Message) error {
	for _, sample := range samples {
		if err := r.register(sample); err != nil {
			return err
		}
	}

	return nil
}

func (r *TypeRegistry) register(sample proto.Message) error {
	if r == nil {
		return fmt.Errorf("type registry is nil")
	}

	name, err := ProtoMessageName(sample)
	if err != nil {
		return err
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return fmt.Errorf("record name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[trimmedName]; exists {
		return fmt.Errorf("message factory already registered for record name: %s", trimmedName)
	}

	r.factories[trimmedName] = func() proto.Message {
		return sample.ProtoReflect().New().Interface()
	}
	return nil
}

func (r *TypeRegistry) NewMessage(name string) (proto.Message, error) {
	if r == nil {
		return nil, fmt.Errorf("type registry is nil")
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, fmt.Errorf("record name is required")
	}

	r.mu.RLock()
	factory, ok := r.factories[trimmedName]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unsupported record name: %s", trimmedName)
	}

	message := factory()
	if message == nil {
		return nil, fmt.Errorf("message factory returned nil for record name: %s", trimmedName)
	}

	return message, nil
}

func ProtoMessageName(msg proto.Message) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("message is nil")
	}

	reflect := msg.ProtoReflect()
	if reflect == nil {
		return "", fmt.Errorf("message reflect is nil")
	}

	descriptor := reflect.Descriptor()
	if descriptor == nil {
		return "", fmt.Errorf("message descriptor is nil")
	}

	return string(descriptor.FullName()), nil
}
