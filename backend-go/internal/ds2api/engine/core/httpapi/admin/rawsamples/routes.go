package rawsamples

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/dev/raw-samples/capture", h.captureRawSample)
	r.Get("/dev/raw-samples/query", h.queryRawSampleCaptures)
	r.Post("/dev/raw-samples/save", h.saveRawSampleFromCaptures)
}
