package snooper

import (
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
	router.PathPrefix("/").Handler(http.DefaultServeMux)
}
