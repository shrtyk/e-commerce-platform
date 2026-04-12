package kafka

import (
	"testing"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
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
