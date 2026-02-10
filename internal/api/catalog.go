package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/alfredtm/gitops-squared/internal/oci"
)

// CatalogManager maintains an in-memory index of all resources
// and assembles the Flux-consumable catalog tarball.
type CatalogManager struct {
	ociClient *oci.Client
	mu        sync.RWMutex
	resources map[string][]byte // "namespace/name" -> YAML bytes
}

// NewCatalogManager creates a new catalog manager.
func NewCatalogManager(client *oci.Client) *CatalogManager {
	return &CatalogManager{
		ociClient: client,
		resources: make(map[string][]byte),
	}
}

// Set adds or updates a resource in the catalog.
func (cm *CatalogManager) Set(namespace, name string, manifest []byte) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.resources[namespace+"/"+name] = manifest
}

// Delete removes a resource from the catalog.
func (cm *CatalogManager) Delete(namespace, name string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.resources, namespace+"/"+name)
}

// Get returns a resource's YAML from the catalog.
func (cm *CatalogManager) Get(namespace, name string) ([]byte, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	data, ok := cm.resources[namespace+"/"+name]
	return data, ok
}

// List returns all resource names and their YAML.
func (cm *CatalogManager) List() map[string][]byte {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	result := make(map[string][]byte, len(cm.resources))
	for k, v := range cm.resources {
		result[k] = v
	}
	return result
}

// PushCatalog builds a tar.gz of all current manifests and pushes it to the registry.
func (cm *CatalogManager) PushCatalog(ctx context.Context) error {
	cm.mu.RLock()
	resources := make(map[string][]byte, len(cm.resources))
	for k, v := range cm.resources {
		resources[k] = v
	}
	cm.mu.RUnlock()

	tarGz, err := buildCatalogTarGz(resources)
	if err != nil {
		return fmt.Errorf("building catalog tarball: %w", err)
	}

	_, err = cm.ociClient.PushCatalog(ctx, tarGz)
	if err != nil {
		return fmt.Errorf("pushing catalog: %w", err)
	}

	log.Printf("Pushed catalog with %d resources", len(resources))
	return nil
}

// Restore rebuilds the in-memory state from the registry on startup.
func (cm *CatalogManager) Restore(ctx context.Context) error {
	repos, err := cm.ociClient.ListResourceRepos(ctx)
	if err != nil {
		return fmt.Errorf("listing resource repos: %w", err)
	}

	restored := 0
	for _, repo := range repos {
		manifest, annotations, err := cm.ociClient.PullResource(ctx, repo.Namespace, repo.Name, "latest")
		if err != nil {
			log.Printf("Warning: failed to pull %s/%s: %v", repo.Namespace, repo.Name, err)
			continue
		}

		if annotations[oci.AnnotationResourceDeleted] == "true" {
			continue
		}

		cm.Set(repo.Namespace, repo.Name, manifest)
		restored++
	}

	log.Printf("Restored %d resources from registry", restored)
	return cm.PushCatalog(ctx)
}

func buildCatalogTarGz(resources map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Collect filenames for the kustomization.yaml.
	var filenames []string

	for key, manifest := range resources {
		filename := strings.ReplaceAll(key, "/", "-") + ".yaml"
		filenames = append(filenames, filename)

		hdr := &tar.Header{
			Name: "manifests/" + filename,
			Mode: 0644,
			Size: int64(len(manifest)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(manifest); err != nil {
			return nil, err
		}
	}

	// Write a kustomization.yaml that references all resources.
	kustomization := buildKustomization(filenames)
	hdr := &tar.Header{
		Name: "manifests/kustomization.yaml",
		Mode: 0644,
		Size: int64(len(kustomization)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(kustomization); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func buildKustomization(filenames []string) []byte {
	var b bytes.Buffer
	b.WriteString("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n")
	for _, f := range filenames {
		b.WriteString("  - " + f + "\n")
	}
	if len(filenames) == 0 {
		b.WriteString("  []\n")
	}
	return b.Bytes()
}
