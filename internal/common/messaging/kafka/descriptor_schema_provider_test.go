package kafka

import (
	"testing"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestDescriptorSchemaProviderSchemaFor(t *testing.T) {
	provider := NewDescriptorSchemaProvider()

	t.Run("product created schema", func(t *testing.T) {
		schema, err := provider.SchemaFor(&catalogv1.ProductCreated{})
		require.NoError(t, err)
		require.NotEmpty(t, schema.Schema)
		require.Equal(t, []int{0}, schema.Index)
		require.Contains(t, schema.Schema, "package ecommerce.catalog.v1;")
		require.Contains(t, schema.Schema, "message ProductCreated")
		require.Contains(t, schema.Schema, "metadata")
		require.Contains(t, schema.Schema, "product_id")
		require.Contains(t, schema.Schema, "sku")
		require.Contains(t, schema.Schema, "name")
	})

	t.Run("product created references", func(t *testing.T) {
		schema, err := provider.SchemaFor(&catalogv1.ProductCreated{})
		require.NoError(t, err)
		require.Equal(t, []string{
			"common/v1/event_metadata.proto",
			"common/v1/money.proto",
			"catalog/v1/types.proto",
		}, referencesSubjects(schema.References))
		require.Equal(t, []string{
			"common/v1/event_metadata.proto",
			"common/v1/money.proto",
			"catalog/v1/types.proto",
		}, referencesNames(schema.References))
		require.Equal(t, []int{0, 0, 0}, referencesVersions(schema.References))

		require.Equal(t, []string{
			"common/v1/event_metadata.proto",
			"common/v1/money.proto",
			"catalog/v1/types.proto",
		}, dependencySubjects(schema.Dependencies))

		typesDependency, ok := dependencyBySubject(schema.Dependencies, "catalog/v1/types.proto")
		require.True(t, ok)
		require.Equal(t, []string{"common/v1/money.proto"}, referencesSubjects(typesDependency.References))
		require.Equal(t, []string{"common/v1/money.proto"}, referencesNames(typesDependency.References))
		require.Equal(t, []int{0}, referencesVersions(typesDependency.References))

		eventMetadataDependency, ok := dependencyBySubject(schema.Dependencies, "common/v1/event_metadata.proto")
		require.True(t, ok)
		require.Empty(t, eventMetadataDependency.References)

		moneyDependency, ok := dependencyBySubject(schema.Dependencies, "common/v1/money.proto")
		require.True(t, ok)
		require.Empty(t, moneyDependency.References)

		for _, reference := range schema.References {
			require.NotContains(t, reference.Name, "google/protobuf/")
			require.NotContains(t, reference.Subject, "google/protobuf/")
		}

		for _, dependency := range schema.Dependencies {
			require.NotContains(t, dependency.Subject, "google/protobuf/")
			for _, reference := range dependency.References {
				require.NotContains(t, reference.Name, "google/protobuf/")
				require.NotContains(t, reference.Subject, "google/protobuf/")
			}
		}
	})

	t.Run("nil message", func(t *testing.T) {
		schema, err := provider.SchemaFor(nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "message is nil")
		require.Equal(t, SchemaDefinition{}, schema)
	})
}

func TestMessageIndex(t *testing.T) {
	file := mustBuildMessageIndexTestFile(t)

	tests := []struct {
		name   string
		target func(file protoreflect.FileDescriptor) protoreflect.MessageDescriptor
		want   []int
	}{
		{
			name: "top-level first",
			target: func(file protoreflect.FileDescriptor) protoreflect.MessageDescriptor {
				return file.Messages().Get(0)
			},
			want: []int{0},
		},
		{
			name: "top-level second",
			target: func(file protoreflect.FileDescriptor) protoreflect.MessageDescriptor {
				return file.Messages().Get(1)
			},
			want: []int{1},
		},
		{
			name: "nested one level deep",
			target: func(file protoreflect.FileDescriptor) protoreflect.MessageDescriptor {
				return file.Messages().Get(2).Messages().Get(1)
			},
			want: []int{2, 1},
		},
		{
			name: "deeply nested",
			target: func(file protoreflect.FileDescriptor) protoreflect.MessageDescriptor {
				return file.Messages().Get(2).Messages().Get(0).Messages().Get(0)
			},
			want: []int{2, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := messageIndex(file, tt.target(file))
			require.Equal(t, tt.want, got)
		})
	}
}

func mustBuildMessageIndexTestFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:  strPtr("proto3"),
		Name:    strPtr("test/message_index.proto"),
		Package: strPtr("test.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: strPtr("TopLevelFirst")},
			{Name: strPtr("TopLevelSecond")},
			{
				Name: strPtr("Outer"),
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name: strPtr("Inner"),
						NestedType: []*descriptorpb.DescriptorProto{
							{Name: strPtr("Deep")},
						},
					},
					{Name: strPtr("Sibling")},
				},
			},
		},
	}

	file, err := protodesc.NewFile(fileProto, nil)
	require.NoError(t, err)

	return file
}

func strPtr(value string) *string {
	return &value
}

func referencesSubjects(references []sr.SchemaReference) []string {
	subjects := make([]string, 0, len(references))
	for _, reference := range references {
		subjects = append(subjects, reference.Subject)
	}

	return subjects
}

func referencesNames(references []sr.SchemaReference) []string {
	names := make([]string, 0, len(references))
	for _, reference := range references {
		names = append(names, reference.Name)
	}

	return names
}

func referencesVersions(references []sr.SchemaReference) []int {
	versions := make([]int, 0, len(references))
	for _, reference := range references {
		versions = append(versions, reference.Version)
	}

	return versions
}

func dependencySubjects(dependencies []SchemaDependency) []string {
	subjects := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		subjects = append(subjects, dependency.Subject)
	}

	return subjects
}

func dependencyBySubject(dependencies []SchemaDependency, subject string) (SchemaDependency, bool) {
	for _, dependency := range dependencies {
		if dependency.Subject == subject {
			return dependency, true
		}
	}

	return SchemaDependency{}, false
}
