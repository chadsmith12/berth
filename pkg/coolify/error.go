package coolify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNoToken is returned when no token is configured
var ErrNoToken = errors.New("no token configured")

type ApiError struct {
	Status     int
	Method     string
	Path       string
	Message    string
	Conflicts  []DomainConflict
	Team       string
	RetryAfter time.Duration
	Body       string
}

type DomainConflict struct {
	Domain       string `json:"domain"`
	ResourceName string `json:"resource_name"`
	ResourceUuid string `json:"resource_uuid"`
	ResourceType string `json:"resource_type"`
	Message      string `json:"message"`
}

func (e *ApiError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "[401] token was rejected. It may be expired, revoked, or invalid."
	case http.StatusForbidden:
		return "[403] token lacks the required permissions - " + e.Message
	case http.StatusNotFound:
		msg := fmt.Sprintf("[404] not found: %s %s", e.Method, e.Path)
		if e.Team != "" {
			return fmt.Sprintf("%s\n\n This may also mean your token is scoped to the wrong team.\nCurrently acting as: %s", msg, e.Team)
		}
		return fmt.Sprintf("%s\n\n This may also mean your token is scoped to the wrong team.", msg)
	case http.StatusConflict:
		if len(e.Conflicts) > 0 {
			var b strings.Builder
			b.WriteString("[409] domain conflict:\n")
			for _, c := range e.Conflicts {
				fmt.Fprintf(&b, " %s is already used by %s %q (%s)\n",
					c.Domain, c.ResourceType, c.ResourceName, c.ResourceUuid)
			}
			b.WriteString("\n Use a different domain, or --force-domain to override.")
			return b.String()
		}
		return "[409] conflict: " + e.Message
	case http.StatusTooManyRequests:
		if e.RetryAfter > 0 {
			return fmt.Sprintf("[429] too many requests: %s. Retry after %s.", e.Message, e.RetryAfter)
		}
		return "[429] too many requests: " + e.Message
	default:
		return fmt.Sprintf("[%d] %s", e.Status, e.Message)
	}
}

func parseError(resp *http.Response, body []byte, team string) *ApiError {
	e := &ApiError{
		Status: resp.StatusCode,
		Team:   team,
		Method: resp.Request.Method,
		Path:   resp.Request.URL.Path,
		Body:   string(body),
	}

	var payload struct {
		Message   string              `json:"message"`
		Conflicts []DomainConflict    `json:"conflicts"`
		Errors    map[string][]string `json:"errors"`
	}

	err := json.Unmarshal(body, &payload)
	if err != nil {
		e.Message = fmt.Sprintf("failed to unmarshal error payload: %s", err)
		return e
	}
	e.Message = payload.Message
	e.Conflicts = payload.Conflicts
	if len(payload.Errors) > 0 {
		e.Message = strings.TrimSpace(e.Message + " — " + flattenFieldErrors(payload.Errors))
	}

	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	return e
}

// flattenFieldErrors renders Coolify's 422 "errors" map deterministically:
// "field: message; other: message". Without it the actionable field is lost
// behind a bare "Validation failed.".
func flattenFieldErrors(errors map[string][]string) string {
	fields := make([]string, 0, len(errors))
	for field := range errors {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, fmt.Sprintf("%s: %s", field, strings.Join(errors[field], "; ")))
	}
	return strings.Join(parts, "; ")
}
