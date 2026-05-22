package server

import (
	"net/http"

	"github.com/source-asia-backend/catalog"
	"github.com/source-asia-backend/middleware"
	"github.com/source-asia-backend/ratelimit"
)

// New wires all dependencies, registers routes, and returns a ready-to-serve
// http.Handler. Keeping the return type as http.Handler (not a custom struct)
// keeps things simple — there is no additional server state beyond the mux.
func New() http.Handler {
	mux := http.NewServeMux()

	// Part 1 – Rate-limited API
	rl := ratelimit.NewHandler(ratelimit.NewStore())
	mux.HandleFunc("POST /request", rl.HandleRequest)
	mux.HandleFunc("GET /stats", rl.HandleStats)

	// Part 2 – Product catalog
	cat := catalog.NewHandler(catalog.NewStore())
	mux.HandleFunc("POST /products", cat.CreateProduct)
	mux.HandleFunc("GET /products", cat.ListProducts)
	mux.HandleFunc("GET /products/{id}", cat.GetProduct)
	mux.HandleFunc("POST /products/{id}/media", cat.AddMedia)

	return middleware.Chain(
		mux,
		middleware.MaxBody(1<<20), // 1 MB body limit on all routes
		middleware.RequireJSON,
		middleware.RequestLogger,
	)
}
