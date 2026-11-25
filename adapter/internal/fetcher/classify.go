package fetcher

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Class is the outcome class of one Get* attempt (spec §9.4).
type Class int

// The classes of spec §9.4, plus Aborted for a shutdown in the middle of a fetch.
const (
	Success         Class = iota // OK, version >= trigger's, entity valid
	Stale                        // OK, version < trigger's: retry, then STALE_VERSION
	InvalidEntity                // OK, entity breaks §6.7: VALIDATION_ERROR
	NotFound                     // NOT_FOUND: dead-letter NOT_FOUND
	InvalidArgument              // INVALID_ARGUMENT: dead-letter VALIDATION_ERROR
	Transient                    // retry without limit, never dead-lettered
	StaleTimeout                 // still stale after STALE_MAX_WAIT_MS: STALE_VERSION
	Aborted                      // the process is stopping: nothing is published or committed
)

func (c Class) String() string {
	return [...]string{"success", "stale", "invalid_entity", "not_found", "invalid_argument", "transient", "stale_timeout", "aborted"}[c]
}

// Permanent reports whether the class ends in a dead letter.
func (c Class) Permanent() bool {
	return c == InvalidEntity || c == NotFound || c == InvalidArgument || c == StaleTimeout
}

// ClassifyError classifies a failed Get* call by its gRPC code (spec §9.4).
// UNAVAILABLE, DEADLINE_EXCEEDED, RESOURCE_EXHAUSTED, ABORTED, INTERNAL,
// UNKNOWN and connection errors (any non-status error) are transient. Codes
// the spec does not list are transient too: losing data is worse than a
// visible stall (spec §9.4).
func ClassifyError(err error) Class {
	st, ok := status.FromError(err)
	if !ok {
		return Transient
	}
	switch st.Code() {
	case codes.NotFound:
		return NotFound
	case codes.InvalidArgument:
		return InvalidArgument
	default:
		return Transient
	}
}

// ClassifyOK classifies a successful Get* call: invalid entities first, then
// the version against the trigger's (spec §9.4).
func ClassifyOK(entityVersion, triggerVersion int64, validationErr error) Class {
	switch {
	case validationErr != nil:
		return InvalidEntity
	case entityVersion < triggerVersion:
		return Stale
	default:
		return Success
	}
}
