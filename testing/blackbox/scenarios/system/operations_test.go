// Package system_test holds the cross-cutting scenarios: rules that must
// hold on every endpoint of a kind (a garage's data is private to its
// members, read-only mode blocks every write) or under load (races), rather
// than one workflow of one module. The endpoints come from the OpenAPI
// document of the proto module the suite already depends on, so a new
// route is covered the day it is added, or the suite fails and says which.
package system_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-proto/openapi"
)

// operation is one method and path template of the public API.
type operation struct {
	Method string
	Path   string
	// Enums maps a path parameter that takes an enum to a valid value.
	Enums map[string]string
}

func (o operation) String() string { return o.Method + " " + o.Path }

// writes reports whether the method changes state.
func (o operation) writes() bool { return o.Method != http.MethodGet }

// garageScoped reports whether the path lives under /garages/{garageId}.
func (o operation) garageScoped() bool {
	return strings.HasPrefix(o.Path, "/api/v1/garages/{garageId}")
}

// url fills the path template: garageID for {garageId}, a fresh random id
// for every other parameter.
func (o operation) url(garageID string) string {
	path := strings.ReplaceAll(o.Path, "{garageId}", garageID)

	for strings.Contains(path, "{") {
		start := strings.Index(path, "{")
		end := strings.Index(path, "}")

		value, ok := o.Enums[path[start+1:end]]
		if !ok {
			value = uuid.NewString()
		}

		path = path[:start] + value + path[end+1:]
	}

	return path
}

// operations lists every operation of the OpenAPI document, sorted.
func operations(t *testing.T) []operation {
	t.Helper()

	type parameter struct {
		Name string   `json:"name"`
		In   string   `json:"in"`
		Enum []string `json:"enum"`
	}

	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []parameter `json:"parameters"`
		} `json:"paths"`
	}

	require.NoError(t, json.Unmarshal(openapi.Spec, &doc))

	methods := map[string]string{
		"get": http.MethodGet, "post": http.MethodPost, "put": http.MethodPut,
		"patch": http.MethodPatch, "delete": http.MethodDelete,
	}

	var ops []operation

	for path, item := range doc.Paths {
		for name, detail := range item {
			method, ok := methods[name]
			if !ok {
				continue
			}

			if pending(path) {
				continue
			}

			op := operation{Method: method, Path: path, Enums: map[string]string{}}

			for _, param := range detail.Parameters {
				for _, value := range param.Enum {
					if param.In == "path" && !strings.HasSuffix(value, "_UNSPECIFIED") {
						op.Enums[param.Name] = value

						break
					}
				}
			}

			ops = append(ops, op)
		}
	}

	sort.Slice(ops, func(i, j int) bool { return ops[i].String() < ops[j].String() })
	require.NotEmpty(t, ops, "the OpenAPI document lists operations")

	return ops
}

// pendingPhase13 are the paths of the Phase 13 endpoints that the backend does
// not serve yet: the contract lists them, the gateway does not route them. Each
// slice of the release takes its own paths out of this list.
var pendingPhase13 = []string{
	"/data-requests",
}

func pending(path string) bool {
	for _, fragment := range pendingPhase13 {
		if strings.Contains(path, fragment) {
			return true
		}
	}

	return false
}

// body is the JSON body a probe sends: an empty message for writes.
func (o operation) body() []byte {
	if o.writes() && o.Method != http.MethodDelete {
		return []byte(`{}`)
	}

	return nil
}
