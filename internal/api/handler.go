package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/alfredtm/gitops-squared/internal/model"
	"github.com/alfredtm/gitops-squared/internal/oci"
	"sigs.k8s.io/yaml"
)

const defaultNamespace = "default"

// Handler holds HTTP handlers for the resource API.
type Handler struct {
	ociClient *oci.Client
	catalog   *CatalogManager
}

// NewHandler creates a new API handler.
func NewHandler(ociClient *oci.Client, catalog *CatalogManager) *Handler {
	return &Handler{
		ociClient: ociClient,
		catalog:   catalog,
	}
}

// RegisterRoutes registers all API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/resources", h.CreateResource)
	mux.HandleFunc("GET /api/v1/resources", h.ListResources)
	mux.HandleFunc("GET /api/v1/resources/{name}", h.GetResource)
	mux.HandleFunc("DELETE /api/v1/resources/{name}", h.DeleteResource)
	mux.HandleFunc("GET /healthz", h.Healthz)
}

// CreateResource handles POST /api/v1/resources.
func (h *Handler) CreateResource(w http.ResponseWriter, r *http.Request) {
	var req model.ResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}

	// Generate a placeholder version for the YAML annotation — the real one comes from the OCI push.
	yamlBytes, err := req.ToKubernetesYAML(defaultNamespace, "pending")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generating YAML: %v", err)
		return
	}

	digest, version, err := h.ociClient.PushResource(r.Context(), defaultNamespace, req.Name, yamlBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pushing to registry: %v", err)
		return
	}

	// Re-generate YAML with the real version.
	yamlBytes, err = req.ToKubernetesYAML(defaultNamespace, version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generating YAML: %v", err)
		return
	}

	// Update catalog and push.
	h.catalog.Set(defaultNamespace, req.Name, yamlBytes)
	if err := h.catalog.PushCatalog(r.Context()); err != nil {
		log.Printf("Warning: failed to push catalog: %v", err)
	}

	resp := model.ResourceResponse{
		Name:       req.Name,
		Version:    version,
		Digest:     digest,
		Repository: fmt.Sprintf("gitops-squared/resources/%s/%s", defaultNamespace, req.Name),
		Spec:       req.Spec,
		CreatedAt:  "",
	}

	writeJSON(w, http.StatusCreated, resp)
	log.Printf("Created resource %s (version=%s, digest=%s)", req.Name, version, digest[:19])
}

// ListResources handles GET /api/v1/resources.
func (h *Handler) ListResources(w http.ResponseWriter, r *http.Request) {
	all := h.catalog.List()

	resources := make([]model.ResourceResponse, 0, len(all))
	for key := range all {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		resources = append(resources, model.ResourceResponse{
			Name: parts[1],
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"resources": resources,
		"count":     len(resources),
	})
}

// GetResource handles GET /api/v1/resources/{name}.
func (h *Handler) GetResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	data, ok := h.catalog.Get(defaultNamespace, name)
	if !ok {
		writeError(w, http.StatusNotFound, "resource %q not found", name)
		return
	}

	resp := model.ResourceResponse{
		Name: name,
	}

	// Parse the stored YAML to extract the spec.
	var pr model.PlatformResource
	if err := yaml.Unmarshal(data, &pr); err == nil {
		resp.Spec = pr.Spec
	}

	writeJSON(w, http.StatusOK, resp)
}

// DeleteResource handles DELETE /api/v1/resources/{name}.
func (h *Handler) DeleteResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if _, ok := h.catalog.Get(defaultNamespace, name); !ok {
		writeError(w, http.StatusNotFound, "resource %q not found", name)
		return
	}

	// Push tombstone artifact for audit trail.
	digest, version, err := h.ociClient.PushTombstone(r.Context(), defaultNamespace, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pushing tombstone: %v", err)
		return
	}

	// Remove from catalog and push.
	h.catalog.Delete(defaultNamespace, name)
	if err := h.catalog.PushCatalog(r.Context()); err != nil {
		log.Printf("Warning: failed to push catalog: %v", err)
	}

	resp := model.ResourceResponse{
		Name:    name,
		Version: version,
		Digest:  digest,
		Deleted: true,
	}

	writeJSON(w, http.StatusOK, resp)
	log.Printf("Deleted resource %s (tombstone version=%s)", name, version)
}

// Healthz handles GET /healthz.
func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{
		"error": fmt.Sprintf(format, args...),
	})
}
