package parameterization

import (
	"math"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestImportedContractsPreserveSourceAndPositionValidation(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"answers": map[string]any{"type": "array", "minItems": 2, "prefixItems": []any{
			map[string]any{"type": "string", "const": "confirmed"},
			map[string]any{"type": "string", "format": "ipv4", "pattern": "^10\\."},
		}, "items": map[string]any{"type": "string", "format": "ipv4-cidr"}},
		"count": map[string]any{"type": "integer", "minimum": float64(0), "maximum": float64(math.MaxUint64), "deprecated": true},
		"port":  map[string]any{"x-kubernetes-int-or-string": true},
	}}
	model := Model{Dialect: Dialect, Schema: schema}
	before := Digest(schema)
	require.Empty(t, ValidateModelSchema(model))
	for _, tc := range []struct {
		answers []any
		valid   bool
	}{
		{[]any{"confirmed", "10.0.0.1"}, true},
		{[]any{"confirmed", "10.0.0.1", "10.1.0.0/24"}, true},
		{[]any{"changed", "10.0.0.1"}, false},
		{[]any{"confirmed", "11.0.0.1"}, false},
		{[]any{"confirmed", "10.0.0.999"}, false},
		{[]any{"confirmed", "10.0.0.1", "10.1.0.0/33"}, false},
		{[]any{"confirmed", "10.0.0.1", "::/64"}, false},
	} {
		input := map[string]any{"answers": tc.answers, "count": float64(0), "port": "https"}
		value, _, errors := Resolve(model, input)
		require.Equal(t, tc.valid, len(errors) == 0, "%v: %v", tc.answers, errors)
		require.Equal(t, input, value)
	}
	_, _, errors := Resolve(model, map[string]any{"count": float64(9007199254740992)})
	require.NotEmpty(t, errors, "schema bounds must not relax the existing input precision limit")
	_, _, errors = Resolve(model, map[string]any{"port": true})
	require.NotEmpty(t, errors)
	require.Equal(t, before, Digest(schema))
	actionSchema := Clone(schema).(map[string]any)
	delete(actionSchema["properties"].(map[string]any), "port") // native string encoding must remain explicit
	encoded, err := FreezeActionContract(actionSchema)
	require.NoError(t, err)
	good := wfv1.AnyString(`["confirmed","10.0.0.1","10.1.0.0/24"]`)
	bad := wfv1.AnyString(`["changed","10.0.0.1"]`)
	require.NoError(t, ValidateActionValues(encoded, []wfv1.Parameter{{Name: "answers", Value: &good}}, nil))
	require.Error(t, ValidateActionValues(encoded, []wfv1.Parameter{{Name: "answers", Value: &bad}}, nil))
}

func TestImportedSchemaRejectsAmbiguityAndUnsafeLiteralData(t *testing.T) {
	for _, schema := range []map[string]any{
		{"type": "string", "format": "unknown"},
		{"type": "array", "prefixItems": []any{}},
		{"type": "array", "prefixItems": []any{map[string]any{}}, "additionalItems": false},
		{"type": "array", "prefixItems": []any{map[string]any{}}, "items": []any{}},
		{"type": "string", "format": "ipv4", "allOf": map[string]any{}},
		{"type": "integer", "deprecated": "true"},
		{"x-kubernetes-int-or-string": "true"},
		{"type": "integer", "default": float64(9007199254740992)},
		{"type": "object", "default": map[string]any{"maximum": float64(9007199254740992)}},
		{"type": "array", "prefixItems": []any{map[string]any{"$ref": "https://invalid.test/schema"}}},
	} {
		require.NotEmpty(t, ValidateModelSchema(Model{Dialect: Dialect, Schema: schema}), "%v", schema)
	}
}

func TestImportedTupleReferencesDefaultsAndTail(t *testing.T) {
	schema := map[string]any{"type": "array", "definitions": map[string]any{"first": map[string]any{"type": "object", "properties": map[string]any{"enabled": map[string]any{"type": "boolean", "default": false}}}}, "prefixItems": []any{map[string]any{"$ref": "#/definitions/first"}}, "items": false}
	model := Model{Dialect: Dialect, Schema: schema}
	value, sources, errors := Resolve(model, []any{map[string]any{}})
	require.Empty(t, errors)
	require.Equal(t, []any{map[string]any{"enabled": false}}, value)
	require.Equal(t, "default", sources["/0/enabled"])
	_, _, errors = Resolve(model, []any{map[string]any{}, "extra"})
	require.NotEmpty(t, errors)
	require.Nil(t, ArrayItemSchema(schema, -1))
	require.NotNil(t, ArrayItemSchema(schema, 0))
	require.Nil(t, ArrayItemSchema(schema, 1))
}
