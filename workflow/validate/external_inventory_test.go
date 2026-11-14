package validate

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	workflowfake "github.com/argoproj/argo-workflows/v4/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-workflows/v4/util/logging"
	"github.com/argoproj/argo-workflows/v4/workflow/templateresolution"
)

// Replay an external, read-only inventory against the actual native validator.
// Keep business definitions and credentials outside the repository. No cluster
// client, Workflow submission, parameter rewrite or provider execution is used.
func TestExternalNativeTemplateInventory(t *testing.T) {
	path := os.Getenv("ARGO_NATIVE_TEMPLATE_INVENTORY")
	if path == "" {
		t.Skip("external native inventory is opt-in")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &list))
	require.NotEmpty(t, list.Items)
	templates := make([]*wfv1.WorkflowTemplate, 0, len(list.Items))
	objects := make([]runtime.Object, 0, len(list.Items))
	for _, item := range list.Items {
		var template wfv1.WorkflowTemplate
		decoder := json.NewDecoder(bytes.NewReader(item))
		decoder.DisallowUnknownFields()
		require.NoError(t, decoder.Decode(&template), "native API must not silently drop newer fields")
		require.Equal(t, "WorkflowTemplate", template.Kind)
		templates = append(templates, &template)
		objects = append(objects, &template)
	}
	client := workflowfake.NewSimpleClientset(objects...)
	clusterTemplates := templateresolution.WrapClusterWorkflowTemplateInterface(client.ArgoprojV1alpha1().ClusterWorkflowTemplates())
	ctx := logging.TestContext(t.Context())
	for _, template := range templates {
		t.Run(template.Name, func(t *testing.T) {
			before := template.DeepCopy()
			getter := templateresolution.WrapWorkflowTemplateInterface(client.ArgoprojV1alpha1().WorkflowTemplates(template.Namespace))
			require.NoError(t, ValidateWorkflowTemplate(ctx, getter, clusterTemplates, template, nil, ValidateOpts{Lint: true}))
			stored, err := client.ArgoprojV1alpha1().WorkflowTemplates(template.Namespace).Get(ctx, template.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, before, stored, "validation must not rewrite the published source")
		})
	}
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb(), "inventory replay must remain read-only")
	}
	t.Logf("validated %d native templates without changing business definitions", len(templates))
}
