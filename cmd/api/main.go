package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/alfredtm/gitops-squared/internal/api"
	"github.com/alfredtm/gitops-squared/internal/oci"
)

func main() {
	registryHost := envOrDefault("REGISTRY_HOST", "localhost:5000")
	listenAddr := envOrDefault("LISTEN_ADDR", ":8080")

	ociClient := oci.NewClient(registryHost, "gitops-squared/resources")
	catalog := api.NewCatalogManager(ociClient)
	handler := api.NewHandler(ociClient, catalog)

	// Restore state from registry on startup.
	ctx := context.Background()
	if err := catalog.Restore(ctx); err != nil {
		log.Printf("Warning: failed to restore catalog from registry: %v", err)
		log.Printf("Starting with empty catalog (registry may not be available yet)")
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	log.Printf("GitOps Squared API server listening on %s", listenAddr)
	log.Printf("Registry: %s", registryHost)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func envOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
