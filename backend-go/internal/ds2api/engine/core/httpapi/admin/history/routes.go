package history

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/chat-history", h.getChatHistory)
	r.Get("/chat-history/{id}", h.getChatHistoryItem)
	r.Delete("/chat-history", h.clearChatHistory)
	r.Delete("/chat-history/{id}", h.deleteChatHistoryItem)
	r.Put("/chat-history/settings", h.updateChatHistorySettings)
	r.Get("/usage", h.getUsage)
	r.Get("/usage/settings", h.getUsageSettings)
	r.Put("/usage/settings", h.updateUsageSettings)
	r.Delete("/usage", h.clearUsage)
}
