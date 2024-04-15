package snooper

import (
	"net/http"

	"github.com/gorilla/mux"
)

type Api struct {
	snooper *Snooper
}

func newApi(snooper *Snooper) *Api {
	return &Api{
		snooper: snooper,
	}
}

func (api *Api) initRouter(router *mux.Router) {

	router.PathPrefix("/").Handler(http.DefaultServeMux)
}
