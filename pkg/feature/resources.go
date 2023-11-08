package feature

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// CreateNamespace will create namespace with the given name if it does not exist yet and sets feature as an owner of it.
// This way we ensure that when the feature is cleaned up, the namespace will be deleted as well.
func CreateNamespace(namespace string, labels ...string) Action {
	return func(f *Feature) error {
		createdNs, err := cluster.CreateNamespace(f.Client, namespace)
		if err != nil {
			return err
		}

		lenLabels := len(labels)
		if lenLabels%2 != 0 {
			return fmt.Errorf("passed labels should have even amount of key/values")
		}

		labelMap := make(map[string]string, lenLabels)
		for i := 0; i < lenLabels; i += 2 {
			key := labels[i]
			value := labels[i+1]
			labelMap[key] = value
		}

		createdNs.SetLabels(labelMap)
		createdNs.SetOwnerReferences([]metav1.OwnerReference{f.OwnerReference()})

		nsClient := f.Clientset.CoreV1().Namespaces()
		_, err = nsClient.Update(context.TODO(), createdNs, metav1.UpdateOptions{})

		return err
	}
}
