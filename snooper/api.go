package snooper

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type API struct {
	snooper *Snooper
}

func newAPI(snooper *Snooper) *API {
	return &API{
		snooper: snooper,
	}
}

func (api *API) initRouter(router *mux.Router) {
	router.HandleFunc("/control", api.snooper.moduleManager.HandleWebSocket)
	router.HandleFunc("/start", api.handleStart).Methods("POST")
	router.HandleFunc("/stop", api.handleStop).Methods("POST")
	router.HandleFunc("/status", api.handleStatus).Methods("GET")
	router.HandleFunc("/block", api.handleBlock).Methods("GET")
	router.HandleFunc("/unblock", api.handleUnblock).Methods("GET")
	router.PathPrefix("/").Handler(http.DefaultServeMux)
}

func (api *API) handleStart(w http.ResponseWriter, _ *http.Request) {
	api.snooper.flowMutex.Lock()
	api.snooper.flowEnabled = true
	api.snooper.flowMutex.Unlock()

	api.snooper.logger.Info("Flow started - proxy requests enabled")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status":  "success",
		"message": "Flow started",
		"enabled": true,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		api.snooper.logger.Errorf("failed writing start response: %v", err)
	}
}

func (api *API) handleStop(w http.ResponseWriter, _ *http.Request) {
	api.snooper.flowMutex.Lock()
	api.snooper.flowEnabled = false
	api.snooper.flowMutex.Unlock()

	api.snooper.logger.Info("Flow stopped - proxy requests disabled")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status":  "success",
		"message": "Flow stopped",
		"enabled": false,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		api.snooper.logger.Errorf("failed writing stop response: %v", err)
	}
}

func (api *API) handleBlock(w http.ResponseWriter, r *http.Request) {
	route := r.URL.Query().Get("route")
	if route == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	api.snooper.flowMutex.Lock()
	api.snooper.flowBlocked[route] = true
	api.snooper.flowMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status":  "success",
		"message": "Flow blocked",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		api.snooper.logger.Errorf("failed writing block response: %v", err)
	}
}

func (api *API) handleUnblock(w http.ResponseWriter, r *http.Request) {
	route := r.URL.Query().Get("route")
	if route == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	api.snooper.flowMutex.Lock()
	delete(api.snooper.flowBlocked, route)
	api.snooper.flowMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status":  "success",
		"message": "Flow unblocked",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		api.snooper.logger.Errorf("failed writing unblock response: %v", err)
	}
}

func (api *API) handleStatus(w http.ResponseWriter, _ *http.Request) {
	api.snooper.flowMutex.RLock()
	enabled := api.snooper.flowEnabled
	api.snooper.flowMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status":  "success",
		"enabled": enabled,
		"message": func() string {
			if enabled {
				return "Flow is enabled"
			}
			return "Flow is disabled"
		}(),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		api.snooper.logger.Errorf("failed writing status response: %v", err)
	}
}
