package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
)

// Client wraps oras-go operations against an OCI registry.
type Client struct {
	registryHost string
	repoPrefix   string // e.g. "gitops-squared/resources"
}

// ResourceInfo holds metadata about a resource artifact in the registry.
type ResourceInfo struct {
	Repository string
	Namespace  string
	Name       string
	Digest     string
	Version    string
}

// NewClient creates a new OCI client.
func NewClient(registryHost, repoPrefix string) *Client {
	return &Client{
		registryHost: registryHost,
		repoPrefix:   repoPrefix,
	}
}

func (c *Client) newRepo(repoPath string) (*remote.Repository, error) {
	ref := fmt.Sprintf("%s/%s", c.registryHost, repoPath)
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("creating repository reference %s: %w", ref, err)
	}
	repo.PlainHTTP = true
	return repo, nil
}

func (c *Client) resourceRepoPath(namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s", c.repoPrefix, namespace, name)
}

// PushResource pushes a resource manifest as an OCI artifact.
// Returns the digest and version tag.
func (c *Client) PushResource(ctx context.Context, namespace, name string, manifest []byte) (string, string, error) {
	repoPath := c.resourceRepoPath(namespace, name)
	repo, err := c.newRepo(repoPath)
	if err != nil {
		return "", "", err
	}

	version := fmt.Sprintf("v%d", time.Now().Unix())
	store := memory.New()

	// Push the YAML blob to the memory store.
	layerDesc, err := oras.PushBytes(ctx, store, MediaTypeResourceYAML, manifest)
	if err != nil {
		return "", "", fmt.Errorf("pushing layer bytes: %w", err)
	}

	layerDesc.Annotations = map[string]string{
		ocispec.AnnotationTitle:     "platformresource.yaml",
		AnnotationResourceName:      name,
		AnnotationResourceNamespace: namespace,
		AnnotationResourceVersion:   version,
	}

	packOpts := oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{layerDesc},
		ManifestAnnotations: map[string]string{
			ocispec.AnnotationCreated:   time.Now().UTC().Format(time.RFC3339),
			AnnotationResourceName:      name,
			AnnotationResourceNamespace: namespace,
		},
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, ArtifactTypeResource, packOpts)
	if err != nil {
		return "", "", fmt.Errorf("packing manifest: %w", err)
	}

	if err := store.Tag(ctx, manifestDesc, version); err != nil {
		return "", "", fmt.Errorf("tagging %s: %w", version, err)
	}

	// Copy from memory store to remote, tagged with version.
	_, err = oras.Copy(ctx, store, version, repo, version, oras.DefaultCopyOptions)
	if err != nil {
		return "", "", fmt.Errorf("pushing to registry: %w", err)
	}

	// Also tag as latest.
	if err := repo.Tag(ctx, manifestDesc, "latest"); err != nil {
		return "", "", fmt.Errorf("tagging latest: %w", err)
	}

	return string(manifestDesc.Digest), version, nil
}

// PushTombstone pushes a deletion marker artifact for a resource.
func (c *Client) PushTombstone(ctx context.Context, namespace, name string) (string, string, error) {
	repoPath := c.resourceRepoPath(namespace, name)
	repo, err := c.newRepo(repoPath)
	if err != nil {
		return "", "", err
	}

	version := fmt.Sprintf("v%d", time.Now().Unix())
	store := memory.New()

	tombstone := []byte(fmt.Sprintf("# deleted: %s/%s\n", namespace, name))
	layerDesc, err := oras.PushBytes(ctx, store, MediaTypeResourceYAML, tombstone)
	if err != nil {
		return "", "", fmt.Errorf("pushing tombstone bytes: %w", err)
	}

	layerDesc.Annotations = map[string]string{
		AnnotationResourceName:      name,
		AnnotationResourceNamespace: namespace,
		AnnotationResourceDeleted:   "true",
		AnnotationResourceVersion:   version,
	}

	packOpts := oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{layerDesc},
		ManifestAnnotations: map[string]string{
			ocispec.AnnotationCreated: time.Now().UTC().Format(time.RFC3339),
			AnnotationResourceDeleted: "true",
		},
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, ArtifactTypeResource, packOpts)
	if err != nil {
		return "", "", fmt.Errorf("packing tombstone manifest: %w", err)
	}

	if err := store.Tag(ctx, manifestDesc, version); err != nil {
		return "", "", fmt.Errorf("tagging %s: %w", version, err)
	}

	_, err = oras.Copy(ctx, store, version, repo, version, oras.DefaultCopyOptions)
	if err != nil {
		return "", "", fmt.Errorf("pushing tombstone to registry: %w", err)
	}

	if err := repo.Tag(ctx, manifestDesc, "latest"); err != nil {
		return "", "", fmt.Errorf("tagging latest: %w", err)
	}

	return string(manifestDesc.Digest), version, nil
}

// PullResource pulls the resource YAML and manifest annotations for a given reference (tag or digest).
func (c *Client) PullResource(ctx context.Context, namespace, name, reference string) ([]byte, map[string]string, error) {
	repoPath := c.resourceRepoPath(namespace, name)
	repo, err := c.newRepo(repoPath)
	if err != nil {
		return nil, nil, err
	}

	// Fetch the manifest.
	desc, rc, err := repo.FetchReference(ctx, reference)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching manifest %s: %w", reference, err)
	}
	defer rc.Close()

	manifestBytes, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("reading manifest: %w", err)
	}

	// Parse the OCI manifest to find layers.
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, nil, fmt.Errorf("parsing manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, nil, fmt.Errorf("manifest %s has no layers", desc.Digest)
	}

	// Pull the first layer (the resource YAML).
	layerDesc := manifest.Layers[0]
	layerRC, err := repo.Fetch(ctx, layerDesc)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching layer: %w", err)
	}
	defer layerRC.Close()

	layerBytes, err := io.ReadAll(layerRC)
	if err != nil {
		return nil, nil, fmt.Errorf("reading layer: %w", err)
	}

	// Merge manifest and layer annotations.
	annotations := make(map[string]string)
	for k, v := range manifest.Annotations {
		annotations[k] = v
	}
	for k, v := range layerDesc.Annotations {
		annotations[k] = v
	}

	return layerBytes, annotations, nil
}

// ListResourceRepos lists all resource repository paths in the registry
// (filtering to only those under the configured prefix, excluding the catalog).
func (c *Client) ListResourceRepos(ctx context.Context) ([]ResourceInfo, error) {
	reg, err := remote.NewRegistry(c.registryHost)
	if err != nil {
		return nil, fmt.Errorf("creating registry: %w", err)
	}
	reg.PlainHTTP = true

	var repos []ResourceInfo
	err = reg.Repositories(ctx, "", func(repoNames []string) error {
		for _, r := range repoNames {
			if !strings.HasPrefix(r, c.repoPrefix+"/") {
				continue
			}
			// Parse namespace/name from suffix.
			suffix := strings.TrimPrefix(r, c.repoPrefix+"/")
			parts := strings.SplitN(suffix, "/", 2)
			if len(parts) != 2 {
				continue
			}
			repos = append(repos, ResourceInfo{
				Repository: r,
				Namespace:  parts[0],
				Name:       parts[1],
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}

	return repos, nil
}

// PushCatalog pushes a tar.gz catalog artifact for Flux consumption.
func (c *Client) PushCatalog(ctx context.Context, tarGzBytes []byte) (string, error) {
	repoPath := "gitops-squared/catalog"
	repo, err := c.newRepo(repoPath)
	if err != nil {
		return "", err
	}

	store := memory.New()

	layerDesc, err := oras.PushBytes(ctx, store, MediaTypeFluxContent, tarGzBytes)
	if err != nil {
		return "", fmt.Errorf("pushing catalog bytes: %w", err)
	}

	// Push an empty config blob with Flux's expected config media type.
	configBytes := []byte("{}")
	configDesc, err := oras.PushBytes(ctx, store, MediaTypeFluxConfig, configBytes)
	if err != nil {
		return "", fmt.Errorf("pushing config bytes: %w", err)
	}

	packOpts := oras.PackManifestOptions{
		Layers:           []ocispec.Descriptor{layerDesc},
		ConfigDescriptor: &configDesc,
		ManifestAnnotations: map[string]string{
			ocispec.AnnotationCreated: time.Now().UTC().Format(time.RFC3339),
		},
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, MediaTypeFluxConfig, packOpts)
	if err != nil {
		return "", fmt.Errorf("packing catalog manifest: %w", err)
	}

	if err := store.Tag(ctx, manifestDesc, "latest"); err != nil {
		return "", fmt.Errorf("tagging catalog: %w", err)
	}

	_, err = oras.Copy(ctx, store, "latest", repo, "latest", oras.DefaultCopyOptions)
	if err != nil {
		return "", fmt.Errorf("pushing catalog to registry: %w", err)
	}

	return string(manifestDesc.Digest), nil
}
