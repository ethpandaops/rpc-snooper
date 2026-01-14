package xatu

import (
	"github.com/sirupsen/logrus"
)

// Router routes JSON-RPC methods to their corresponding event handlers.
type Router struct {
	handlers []EventHandler
	log      logrus.FieldLogger
}

// NewRouter creates a new Router instance.
func NewRouter(log logrus.FieldLogger) *Router {
	return &Router{
		handlers: make([]EventHandler, 0),
		log:      log.WithField("component", "xatu_router"),
	}
}

// Register adds a handler to the router.
func (r *Router) Register(handler EventHandler) {
	r.handlers = append(r.handlers, handler)
	r.log.WithField("handler", handler.Name()).Debug("registered event handler")
}

// RouteRequest finds a matching handler for the request and calls HandleRequest.
// Returns the matched handler (or nil) and whether a handler was matched.
func (r *Router) RouteRequest(event *RequestEvent) (EventHandler, bool) {
	for _, handler := range r.handlers {
		if handler.MethodMatcher()(event.Method) {
			shouldProcessResponse := handler.HandleRequest(event)
			if shouldProcessResponse {
				return handler, true
			}

			return nil, false
		}
	}

	return nil, false
}

// HandlerCount returns the number of registered handlers.
func (r *Router) HandlerCount() int {
	return len(r.handlers)
}
