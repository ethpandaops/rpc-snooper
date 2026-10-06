package snooper

import (
	"time"

	"github.com/sirupsen/logrus"
)

const (
	callOutcomeCompleted        = "completed"
	callOutcomeCanceled         = "canceled"
	callOutcomeDeadlineExceeded = "deadline_exceeded"
	callOutcomeRequestError     = "request_error"
	callOutcomeResponseError    = "response_error"
)

func (s *Snooper) recordCallOutcome(callCtx *ProxyCallContext, outcome string, elapsed time.Duration) {
	if s.metricsEnabled {
		s.callOutcomeMetrics.Finish(s.target.Host, outcome, elapsed)
	}

	// This is independent of asynchronous request/response processing. callidx
	// correlates it with those logs without reading request data concurrently.
	s.logger.WithFields(logrus.Fields{
		"callidx":    callCtx.ID(),
		"outcome":    outcome,
		"elapsed_ms": float64(elapsed) / float64(time.Millisecond),
	}).Info("CALL-OUTCOME")
}
