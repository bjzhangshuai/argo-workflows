package parameterization

import (
	"encoding/json"
	"testing"

	api "github.com/argoproj/argo-workflows/v4/pkg/apis/parameterization/v1alpha1"
	p "github.com/argoproj/argo-workflows/v4/util/parameterization"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
)

func TestImportedTupleFormUsesEachPosition(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"answers": map[string]any{"type": "array", "prefixItems": []any{
			map[string]any{"type": "string", "const": "reviewed"},
			map[string]any{"type": "integer", "minimum": float64(1)},
		}, "items": map[string]any{"type": "boolean"}},
	}}
	model := p.Model{Dialect: p.Dialect, Schema: schema}
	form := api.WorkflowFormSpec{Fields: []api.FormField{
		{Path: "/answers/0", Widget: "text", ReadOnly: true},
		{Path: "/answers/1", Widget: "number"},
		{Path: "/answers/2", Widget: "checkbox"},
	}}
	require.NoError(t, validateForm(form, model, ""))
	require.Equal(t, "reviewed", formSchemaAt(schema, "/answers/0")["const"])
	form.Fields[1].Widget = "checkbox"
	require.Error(t, validateForm(form, model, ""))
}

func TestImportedResultConstraintsCannotBeSilentlyIgnored(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"type": "array", "prefixItems": []any{
		map[string]any{"const": "reviewed"}, map[string]any{"type": "string", "format": "ipv4-cidr"},
	}, "items": false, "minItems": 2})
	require.NoError(t, err)
	var definition api.ActionResultDefinition
	definition.Schema.Raw = raw
	compiled, err := resultSchema(definition)
	require.NoError(t, err)
	for _, tc := range []struct {
		value []any
		valid bool
	}{
		{[]any{"reviewed", "10.0.0.0/24"}, true},
		{[]any{"wrong", "10.0.0.0/24"}, false},
		{[]any{"reviewed", "10.0.0.0/33"}, false},
		{[]any{"reviewed", "10.0.0.0/24", "extra"}, false},
	} {
		result, err := compiled.Validate(gojsonschema.NewGoLoader(tc.value))
		require.NoError(t, err)
		require.Equal(t, tc.valid, result.Valid())
	}
}
