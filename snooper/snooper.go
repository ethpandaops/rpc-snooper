package snooper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"
)

type Snooper struct {
	CallTimeout time.Duration

	target *url.URL
	logger logrus.FieldLogger
	api    *Api

	callIndexCounter uint64
	callIndexMutex   sync.Mutex
}

func NewSnooper(target string, logger logrus.FieldLogger) (*Snooper, error) {
	targetUrl, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	return &Snooper{
		CallTimeout: 60 * time.Second,

		target: targetUrl,
		logger: logger,
	}, nil
}

func (s *Snooper) StartServer(host string, port int, noApi bool) error {
	router := mux.NewRouter()

	if !noApi {
		s.api = newApi(s)
		s.api.initRouter(router.PathPrefix("/_snooper/").Subrouter())
	}

	router.PathPrefix("/").Handler(s)

	n := negroni.New()
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	srv := &http.Server{
		Addr:    fmt.Sprintf("%v:%v", host, port),
		Handler: n,
	}

	logrus.Printf("listening on: %v", srv.Addr)
	return srv.ListenAndServe()
}

func (s *Snooper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//s.logger.Infof("snooper call: %v", r.URL.String())
	err := s.processProxyCall(w, r)
	if err != nil {
		s.logger.Errorf("call failed: %v", err)

		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		j := json.NewEncoder(w)
		j.Encode(map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
	}
}
