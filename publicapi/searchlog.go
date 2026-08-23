package publicapi

import (
	"net/http"
	"time"

	"github.com/eason4kim-rocket/annolux/models"
	"github.com/eason4kim-rocket/annolux/querylog"
)

// OpsMarkerHeader flags operator traffic — smoke probes and staged
// evaluations — so it never pollutes the real query distribution. Setting it
// only suppresses telemetry: billing, limits, and results are unaffected, so
// a caller abusing it gains nothing but absence from statistics.
const OpsMarkerHeader = "X-Annolux-Ops"

// searchOutcome is everything the query log needs from one handled request.
// Handlers fill it at their terminal branches; a zero errorCode means the
// search succeeded.
type searchOutcome struct {
	status      int
	errorCode   string
	response    *publicSearchResponse
	observation *models.SearchObservation
	elapsed     time.Duration
}

// logSearch converts one handled Search request into a query-log record and
// enqueues it without blocking. It runs only for requests that reached the
// engine: auth, validation, and quota rejections say nothing about retrieval
// and would only bury the distribution the log exists to reveal.
func (server *Server) logSearch(request *http.Request, source, accountID, requestID string, publicRequest publicSearchRequest, outcome searchOutcome) {
	if server.querylog == nil || request.Header.Get(OpsMarkerHeader) != "" {
		return
	}
	record := querylog.Record{
		RequestID:     requestID,
		Time:          time.Now().UTC(),
		Source:        source,
		AccountID:     accountID,
		QueryText:     publicRequest.Query,
		Domains:       publicRequest.Domains,
		HTTPStatus:    outcome.status,
		ErrorCode:     outcome.errorCode,
		RankerVersion: server.version,
		LatencyMS:     outcome.elapsed.Milliseconds(),
	}
	if observation := outcome.observation; observation != nil {
		record.QueryLang = observation.QueryLang
		record.Candidates = observation.CandidateCount
		record.ScoreGap = observation.ScoreGap
		record.Signals = querylog.Signals{
			DomainFiltered: observation.DomainFiltered,
			NearDupRemoved: observation.NearDupRemoved,
			OtherLangHits:  observation.OtherLangHits,
			Fused:          observation.Fused,
			Tier1Hits:      observation.Tier1Hits,
		}
		record.TopDocs = make([]querylog.TopDoc, 0, len(observation.Served))
		for index, served := range observation.Served {
			record.TopDocs = append(record.TopDocs, querylog.TopDoc{
				URLHash: querylog.HashURL(served.URL), Rank: index + 1,
				Score: served.Score, Lang: served.Lang,
			})
		}
	}
	if response := outcome.response; response != nil {
		record.ResultCount = len(response.Results)
		record.Signals.Deduplicated = response.Deduplicated
		record.Signals.Partial = response.Partial
		record.Signals.ProviderMs = response.Timing.ProviderMs
		record.Signals.TotalMs = response.Timing.TotalMs
	}
	server.querylog.Log(record)
}
