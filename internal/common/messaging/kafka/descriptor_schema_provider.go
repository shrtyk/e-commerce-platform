package kafka

import (
	"fmt"
	"strings"

	"github.com/jhump/protoreflect/v2/protoprint"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type DescriptorSchemaProvider struct {
	printer protoprint.Printer
}

func NewDescriptorSchemaProvider() *DescriptorSchemaProvider {
	return &DescriptorSchemaProvider{}
}

func (p *DescriptorSchemaProvider) SchemaFor(message proto.Message) (SchemaDefinition, error) {
	if message == nil {
		return SchemaDefinition{}, fmt.Errorf("message is nil")
	}

	messageReflect := message.ProtoReflect()
	if messageReflect == nil {
		return SchemaDefinition{}, fmt.Errorf("message reflect is nil")
	}

	messageDescriptor := messageReflect.Descriptor()
	if messageDescriptor == nil {
		return SchemaDefinition{}, fmt.Errorf("message descriptor is nil")
	}

	rootFile := messageDescriptor.ParentFile()
	if rootFile == nil {
		return SchemaDefinition{}, fmt.Errorf("parent file descriptor is nil")
	}

	orderedDescriptors, err := orderedFileDescriptors(rootFile)
	if err != nil {
		return SchemaDefinition{}, fmt.Errorf("resolve file descriptors: %w", err)
	}

	if len(orderedDescriptors) == 0 {
		return SchemaDefinition{}, fmt.Errorf("ordered file descriptors are empty")
	}

	schemaText, err := p.printer.PrintProtoToString(rootFile)
	if err != nil {
		return SchemaDefinition{}, fmt.Errorf("print schema: %w", err)
	}

	dependencies := collectNonWellKnownDependencies(orderedDescriptors, rootFile.Path())
	references := make([]sr.SchemaReference, 0, len(dependencies))
	schemaDependencies := make([]SchemaDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		dependencySchemaText, err := p.printer.PrintProtoToString(dependency)
		if err != nil {
			return SchemaDefinition{}, fmt.Errorf("print dependency schema %q: %w", dependency.Path(), err)
		}

		dependencyReferences := make([]sr.SchemaReference, 0, dependency.Imports().Len())
		for i := 0; i < dependency.Imports().Len(); i++ {
			imported := dependency.Imports().Get(i)
			importPath := imported.Path()
			if isWellKnownProtoFile(importPath) {
				continue
			}

			dependencyReferences = append(dependencyReferences, sr.SchemaReference{
				Name:    importPath,
				Subject: importPath,
				Version: 0,
			})
		}

		references = append(references, sr.SchemaReference{
			Name:    dependency.Path(),
			Subject: dependency.Path(),
			Version: 0,
		})

		schemaDependencies = append(schemaDependencies, SchemaDependency{
			Subject:    dependency.Path(),
			Schema:     dependencySchemaText,
			References: dependencyReferences,
		})
	}

	return SchemaDefinition{
		Schema:       schemaText,
		References:   references,
		Dependencies: schemaDependencies,
		Index:        messageIndex(rootFile, messageDescriptor),
	}, nil
}

func messageIndex(file protoreflect.FileDescriptor, target protoreflect.MessageDescriptor) []int {
	if file == nil || target == nil {
		return nil
	}

	messages := file.Messages()
	path := make([]int, 0, 4)
	for i := 0; i < messages.Len(); i++ {
		path = append(path, i)
		if index := nestedMessageIndex(messages.Get(i), target, path); index != nil {
			return index
		}
		path = path[:len(path)-1]
	}

	return nil
}

func nestedMessageIndex(
	current protoreflect.MessageDescriptor,
	target protoreflect.MessageDescriptor,
	path []int,
) []int {
	if current.FullName() == target.FullName() {
		return append([]int(nil), path...)
	}

	nested := current.Messages()
	for i := 0; i < nested.Len(); i++ {
		path = append(path, i)
		if index := nestedMessageIndex(nested.Get(i), target, path); index != nil {
			return index
		}
		path = path[:len(path)-1]
	}

	return nil
}

func collectNonWellKnownDependencies(
	ordered []protoreflect.FileDescriptor,
	rootPath string,
) []protoreflect.FileDescriptor {
	dependencies := make([]protoreflect.FileDescriptor, 0, len(ordered))
	for _, fileDescriptor := range ordered {
		path := fileDescriptor.Path()
		if path == rootPath || isWellKnownProtoFile(path) {
			continue
		}

		dependencies = append(dependencies, fileDescriptor)
	}

	return dependencies
}

func orderedFileDescriptors(rootFile protoreflect.FileDescriptor) ([]protoreflect.FileDescriptor, error) {
	visited := make(map[string]struct{})
	ordered := make([]protoreflect.FileDescriptor, 0)

	if err := visitFileDescriptor(rootFile, visited, &ordered); err != nil {
		return nil, err
	}

	return ordered, nil
}

func visitFileDescriptor(
	fileDescriptor protoreflect.FileDescriptor,
	visited map[string]struct{},
	ordered *[]protoreflect.FileDescriptor,
) error {
	if fileDescriptor == nil {
		return fmt.Errorf("file descriptor is nil")
	}

	path := fileDescriptor.Path()
	if path == "" {
		return fmt.Errorf("file descriptor path is empty")
	}

	if _, ok := visited[path]; ok {
		return nil
	}

	visited[path] = struct{}{}

	imports := fileDescriptor.Imports()
	for i := 0; i < imports.Len(); i++ {
		if err := visitFileDescriptor(imports.Get(i), visited, ordered); err != nil {
			return fmt.Errorf("visit dependency %q: %w", imports.Get(i).Path(), err)
		}
	}

	*ordered = append(*ordered, fileDescriptor)
	return nil
}

func isWellKnownProtoFile(path string) bool {
	return strings.HasPrefix(path, "google/protobuf/") ||
		strings.HasPrefix(path, "google/type/") ||
		path == "google/protobuf/descriptor.proto"
}
