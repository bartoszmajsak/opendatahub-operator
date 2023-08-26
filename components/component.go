package components

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Component struct {
	// Set to "true" to enable component, "false" to disable component. A disabled component will not be installed.
	Enabled bool `json:"enabled"`
	// Add any other common fields across components below
}

func (c *Component) IsEnabled() bool {
	return c.Enabled
}

type ComponentInterface interface {
	ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme, namespace string) error
	IsEnabled() bool
	GetComponentName() string
	SetImageParamsMap(imageMap map[string]string) map[string]string
}
