package oci

const (
	// ArtifactTypeResource is the OCI artifact type for platform resources.
	ArtifactTypeResource = "application/vnd.gitops-squared.resource.v1"

	// ArtifactTypeCatalog is the OCI artifact type for the Flux catalog.
	ArtifactTypeCatalog = "application/vnd.gitops-squared.catalog.v1"

	// MediaTypeResourceYAML is the media type for resource YAML layers.
	MediaTypeResourceYAML = "application/vnd.gitops-squared.manifest.v1+yaml"

	// MediaTypeFluxContent is the media type Flux expects for OCI source tarballs.
	MediaTypeFluxContent = "application/vnd.cncf.flux.content.v1.tar+gzip"

	// MediaTypeFluxConfig is the config media type Flux uses for OCI artifacts.
	MediaTypeFluxConfig = "application/vnd.cncf.flux.config.v1+json"

	// AnnotationResourceName is the annotation key for the resource name.
	AnnotationResourceName = "io.gitops-squared.resource.name"

	// AnnotationResourceNamespace is the annotation key for the resource namespace.
	AnnotationResourceNamespace = "io.gitops-squared.resource.namespace"

	// AnnotationResourceVersion is the annotation key for the resource version.
	AnnotationResourceVersion = "io.gitops-squared.resource.version"

	// AnnotationResourceDeleted marks a tombstone artifact.
	AnnotationResourceDeleted = "io.gitops-squared.resource.deleted"
)
