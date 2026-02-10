package model

import (
	"fmt"
	"time"

	"sigs.k8s.io/yaml"
)

// ResourceSpec is the user-facing spec for a platform resource.
type ResourceSpec struct {
	Type     string `json:"type"`
	Size     string `json:"size"`
	Region   string `json:"region,omitempty"`
	Replicas int    `json:"replicas,omitempty"`
}

// ResourceRequest is the JSON body for creating/updating a resource via the API.
type ResourceRequest struct {
	Name string       `json:"name"`
	Spec ResourceSpec `json:"spec"`
}

// ResourceResponse is the JSON response from the API.
type ResourceResponse struct {
	Name       string       `json:"name"`
	Version    string       `json:"version,omitempty"`
	Digest     string       `json:"digest,omitempty"`
	Repository string       `json:"repository,omitempty"`
	Spec       ResourceSpec `json:"spec"`
	CreatedAt  string       `json:"createdAt,omitempty"`
	Deleted    bool         `json:"deleted,omitempty"`
}

// PlatformResource is the Kubernetes CRD representation.
type PlatformResource struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Metadata   PlatformResourceMetadata `json:"metadata"`
	Spec       ResourceSpec             `json:"spec"`
}

// PlatformResourceMetadata holds Kubernetes object metadata fields.
type PlatformResourceMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

var validTypes = map[string]bool{"vm": true, "database": true, "bucket": true}
var validSizes = map[string]bool{"small": true, "medium": true, "large": true}

// Validate checks the resource request for required fields and valid values.
func (r *ResourceRequest) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !validTypes[r.Spec.Type] {
		return fmt.Errorf("invalid type %q: must be one of vm, database, bucket", r.Spec.Type)
	}
	if !validSizes[r.Spec.Size] {
		return fmt.Errorf("invalid size %q: must be one of small, medium, large", r.Spec.Size)
	}
	if r.Spec.Replicas > 10 {
		return fmt.Errorf("replicas must be between 1 and 10")
	}
	return nil
}

// ToKubernetesYAML converts a resource request into a PlatformResource CRD YAML.
func (r *ResourceRequest) ToKubernetesYAML(namespace, version string) ([]byte, error) {
	if r.Spec.Replicas == 0 {
		r.Spec.Replicas = 1
	}

	pr := PlatformResource{
		APIVersion: "gitops-squared.io/v1alpha1",
		Kind:       "PlatformResource",
		Metadata: PlatformResourceMetadata{
			Name:      r.Name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "gitops-squared",
			},
			Annotations: map[string]string{
				"gitops-squared.io/version":   version,
				"gitops-squared.io/pushed-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: r.Spec,
	}

	return yaml.Marshal(pr)
}
