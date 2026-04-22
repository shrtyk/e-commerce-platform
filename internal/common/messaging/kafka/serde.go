package kafka

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
)

type SchemaDefinition struct {
	Schema       string
	References   []sr.SchemaReference
	Dependencies []SchemaDependency
	Index        []int
}

type SchemaDependency struct {
	Subject    string
	Schema     string
	References []sr.SchemaReference
}

type SchemaProvider interface {
	SchemaFor(message proto.Message) (SchemaDefinition, error)
}

type schemaRegistry interface {
	CreateSchema(ctx context.Context, subject string, s sr.Schema) (sr.SubjectSchema, error)
}

type ProtoSerde struct {
	registry       schemaRegistry
	provider       SchemaProvider
	serde          *sr.Serde
	registeredType map[string]struct{}
	mu             sync.RWMutex
}

func NewProtoSerde(registry schemaRegistry, provider SchemaProvider) *ProtoSerde {
	return &ProtoSerde{
		registry:       registry,
		provider:       provider,
		serde:          sr.NewSerde(sr.Header(&sr.ConfluentHeader{})),
		registeredType: make(map[string]struct{}),
	}
}

func TopicRecordNameSubject(topic, recordFullName string) string {
	return fmt.Sprintf("%s-%s", topic, recordFullName)
}

func (s *ProtoSerde) RegisterType(ctx context.Context, topic string, prototype proto.Message) error {
	if s == nil {
		return wrapNonRetriable(errors.New("proto serde is nil"), "register type")
	}

	if prototype == nil {
		return wrapNonRetriable(errors.New("prototype is nil"), "register type")
	}

	if topic == "" {
		return wrapNonRetriable(errors.New("topic is empty"), "register type")
	}

	if s.registry == nil {
		return wrapNonRetriable(errors.New("schema registry client is nil"), "register type")
	}

	if s.provider == nil {
		return wrapNonRetriable(errors.New("schema provider is nil"), "register type")
	}

	fullName := prototype.ProtoReflect().Descriptor().FullName()
	subject := TopicRecordNameSubject(topic, string(fullName))

	s.mu.RLock()
	_, registered := s.registeredType[subject]
	s.mu.RUnlock()
	if registered {
		return nil
	}

	schemaDef, err := s.provider.SchemaFor(prototype)
	if err != nil {
		return wrapNonRetriable(fmt.Errorf("resolve schema for %s: %w", fullName, err), "register type")
	}

	if schemaDef.Schema == "" {
		return wrapNonRetriable(fmt.Errorf("empty schema for %s", fullName), "register type")
	}

	resolvedReferences, err := s.registerDependencies(ctx, schemaDef)
	if err != nil {
		return err
	}

	subjectSchema, err := s.registry.CreateSchema(ctx, subject, sr.Schema{
		Schema:     schemaDef.Schema,
		Type:       sr.TypeProtobuf,
		References: resolvedReferences,
	})
	if err != nil {
		classified := ClassifyError(err)
		if IsRetriable(classified) {
			return wrapRetriable(fmt.Errorf("create schema %s: %w", subject, err), "register type")
		}

		return wrapNonRetriable(fmt.Errorf("create schema %s: %w", subject, err), "register type")
	}

	index := schemaDef.Index
	if len(index) == 0 {
		index = []int{0}
	}

	prototypeType := reflect.TypeOf(prototype)
	if prototypeType.Kind() != reflect.Pointer {
		return wrapNonRetriable(fmt.Errorf("prototype %s must be pointer", fullName), "register type")
	}

	s.serde.Register(
		subjectSchema.ID,
		prototype,
		sr.Index(index...),
		sr.EncodeFn(func(value any) ([]byte, error) {
			message, ok := value.(proto.Message)
			if !ok {
				return nil, fmt.Errorf("value %T is not proto message", value)
			}

			encoded, marshalErr := proto.Marshal(message)
			if marshalErr != nil {
				return nil, fmt.Errorf("marshal %T: %w", message, marshalErr)
			}

			return encoded, nil
		}),
		sr.DecodeFn(func(data []byte, target any) error {
			message, ok := target.(proto.Message)
			if !ok {
				return fmt.Errorf("target %T is not proto message", target)
			}

			if unmarshalErr := proto.Unmarshal(data, message); unmarshalErr != nil {
				return fmt.Errorf("unmarshal %T: %w", message, unmarshalErr)
			}

			return nil
		}),
		sr.GenerateFn(func() any {
			return reflect.New(prototypeType.Elem()).Interface()
		}),
	)

	s.mu.Lock()
	s.registeredType[subject] = struct{}{}
	s.mu.Unlock()

	return nil
}

func (s *ProtoSerde) RegisterTypes(ctx context.Context, topic string, samples ...proto.Message) error {
	for _, sample := range samples {
		if err := s.RegisterType(ctx, topic, sample); err != nil {
			return err
		}
	}

	return nil
}

func (s *ProtoSerde) registerDependencies(ctx context.Context, schemaDef SchemaDefinition) ([]sr.SchemaReference, error) {
	if len(schemaDef.References) == 0 {
		return nil, nil
	}

	resolvedVersions := make(map[string]int, len(schemaDef.Dependencies))
	for _, dependency := range schemaDef.Dependencies {
		if dependency.Subject == "" {
			return nil, wrapNonRetriable(errors.New("dependency subject is empty"), "register type")
		}

		if _, exists := resolvedVersions[dependency.Subject]; exists {
			continue
		}

		if dependency.Schema == "" {
			return nil, wrapNonRetriable(fmt.Errorf("empty dependency schema for %s", dependency.Subject), "register type")
		}

		resolvedDependencyReferences := make([]sr.SchemaReference, 0, len(dependency.References))
		for _, reference := range dependency.References {
			version, ok := resolvedVersions[reference.Subject]
			if !ok {
				return nil, wrapNonRetriable(
					fmt.Errorf("dependency reference %s for %s is not registered", reference.Subject, dependency.Subject),
					"register type",
				)
			}

			resolvedDependencyReferences = append(resolvedDependencyReferences, sr.SchemaReference{
				Name:    reference.Name,
				Subject: reference.Subject,
				Version: version,
			})
		}

		subjectSchema, err := s.registry.CreateSchema(ctx, dependency.Subject, sr.Schema{
			Schema:     dependency.Schema,
			Type:       sr.TypeProtobuf,
			References: resolvedDependencyReferences,
		})
		if err != nil {
			classified := ClassifyError(err)
			if IsRetriable(classified) {
				return nil, wrapRetriable(fmt.Errorf("create schema %s: %w", dependency.Subject, err), "register type")
			}

			return nil, wrapNonRetriable(fmt.Errorf("create schema %s: %w", dependency.Subject, err), "register type")
		}

		resolvedVersions[dependency.Subject] = subjectSchema.Version
	}

	resolvedRootReferences := make([]sr.SchemaReference, 0, len(schemaDef.References))
	for _, reference := range schemaDef.References {
		version, ok := resolvedVersions[reference.Subject]
		if !ok {
			return nil, wrapNonRetriable(
				fmt.Errorf("unresolved root reference %s: dependency graph/order mismatch", reference.Subject),
				"register type",
			)
		}

		resolvedRootReferences = append(resolvedRootReferences, sr.SchemaReference{
			Name:    reference.Name,
			Subject: reference.Subject,
			Version: version,
		})
	}

	return resolvedRootReferences, nil
}

func (s *ProtoSerde) Encode(ctx context.Context, topic string, message proto.Message) ([]byte, string, error) {
	if s == nil {
		return nil, "", wrapNonRetriable(errors.New("proto serde is nil"), "encode protobuf")
	}

	if message == nil {
		return nil, "", wrapNonRetriable(errors.New("message is nil"), "encode protobuf")
	}

	fullName := string(message.ProtoReflect().Descriptor().FullName())
	if registerErr := s.RegisterType(ctx, topic, message); registerErr != nil {
		return nil, "", fmt.Errorf("register message type %s: %w", fullName, registerErr)
	}

	encoded, err := s.serde.Encode(message)
	if err == nil {
		return encoded, fullName, nil
	}

	if !errors.Is(err, sr.ErrNotRegistered) {
		return nil, "", wrapNonRetriable(fmt.Errorf("encode %s: %w", fullName, err), "encode protobuf")
	}

	encoded, err = s.serde.Encode(message)
	if err != nil {
		return nil, "", wrapNonRetriable(fmt.Errorf("encode %s after registration: %w", fullName, err), "encode protobuf")
	}

	return encoded, fullName, nil
}

func (s *ProtoSerde) Decode(data []byte) (proto.Message, error) {
	if s == nil {
		return nil, wrapNonRetriable(errors.New("proto serde is nil"), "decode protobuf")
	}

	decoded, err := s.serde.DecodeNew(data)
	if err != nil {
		if errors.Is(err, sr.ErrNotRegistered) {
			return nil, wrapNonRetriable(err, "decode protobuf")
		}

		return nil, wrapNonRetriable(fmt.Errorf("decode payload: %w", err), "decode protobuf")
	}

	message, ok := decoded.(proto.Message)
	if !ok {
		return nil, wrapNonRetriable(fmt.Errorf("decoded value %T is not proto message", decoded), "decode protobuf")
	}

	return message, nil
}
