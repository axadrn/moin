package api_test

import (
	"bufio"
	"bytes"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
	"gopkg.in/yaml.v3"
)

var (
	openAPIPathLine   = regexp.MustCompile(`^  (/[^:]*):$`)
	openAPIMethodLine = regexp.MustCompile(`^    (get|post|put|patch|delete):$`)
	openAPIRights     = regexp.MustCompile(`^        - bearerAuth: \[([a-z, ]+)\]$`)
	pathParameter     = regexp.MustCompile(`\{[^}]+\}`)
)

// documentedOperations returns every "METHOD /path" of the OpenAPI document
// under /api/v1.
func documentedOperations(t *testing.T) []string {
	t.Helper()
	var operations []string
	currentPath := ""
	scanner := bufio.NewScanner(bytes.NewReader(api.OpenAPIDocument))
	for scanner.Scan() {
		line := scanner.Text()
		pathMatch := openAPIPathLine.FindStringSubmatch(line)
		if pathMatch != nil {
			currentPath = pathMatch[1]
			continue
		}
		if line != "" && !strings.HasPrefix(line, "    ") {
			currentPath = ""
			continue
		}
		methodMatch := openAPIMethodLine.FindStringSubmatch(line)
		if methodMatch != nil && strings.HasPrefix(currentPath, "/api/v1/") {
			operations = append(operations, strings.ToUpper(methodMatch[1])+" "+currentPath)
		}
	}
	err := scanner.Err()
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) == 0 {
		t.Fatal("the OpenAPI document lists no operations under /api/v1")
	}
	return operations
}

// TestOpenAPIDocumentsEveryRoute keeps the router and the OpenAPI document
// in sync: every route has an operation and every operation has a route.
func TestOpenAPIDocumentsEveryRoute(t *testing.T) {
	environment := newAPIEnvironment(t)
	documented := documentedOperations(t)

	registered := make([]string, 0, len(environment.routes))
	for _, route := range environment.routes {
		if slices.Contains(registered, route.Pattern) {
			t.Errorf("route %q is registered twice", route.Pattern)
		}
		registered = append(registered, route.Pattern)
		if !strings.Contains(route.Pattern, " /api/v1/") {
			t.Errorf("route %q is not under /api/v1", route.Pattern)
		}
	}

	for _, pattern := range registered {
		if !slices.Contains(documented, pattern) {
			t.Errorf("route %q is missing from openapi.yaml", pattern)
		}
	}
	for _, operation := range documented {
		if !slices.Contains(registered, operation) {
			t.Errorf("openapi.yaml documents %q, but the router has no such route", operation)
		}
	}
}

// TestOpenAPIOperationsReachTheirRoute sends every documented operation
// through the router and checks that it reaches a route instead of the API
// not found handler.
func TestOpenAPIOperationsReachTheirRoute(t *testing.T) {
	environment := newAPIEnvironment(t)
	for _, operation := range documentedOperations(t) {
		method, path, _ := strings.Cut(operation, " ")
		path = pathParameter.ReplaceAllString(path, "unknown")
		recorder := environment.send(t, apiRequest{method: method, path: path})
		if recorder.Code == http.StatusMethodNotAllowed || strings.Contains(recorder.Body.String(), "The requested API endpoint does not exist.") {
			t.Errorf("%s did not reach a route: %d %s", operation, recorder.Code, recorder.Body.String())
		}
	}
}

func TestOpenAPIDocumentIsServed(t *testing.T) {
	environment := newAPIEnvironment(t)

	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/openapi.yaml"})

	expectStatus(t, recorder, http.StatusOK)
	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/yaml" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if !bytes.HasPrefix(recorder.Body.Bytes(), []byte("openapi: 3.1.0\n")) {
		t.Fatalf("body does not start with the OpenAPI version: %.40q", recorder.Body.String())
	}
}

// documentedTokenAccess describes what the OpenAPI document says about
// connections and CI tokens on one operation.
type documentedTokenAccess struct {
	// rights are the roles of its bearerAuth entry, empty when it has none.
	rights string
	// public is set for operations with "security: []".
	public bool
	// forbidden is set when the operation documents a 403 answer.
	forbidden bool
}

// documentedTokenAccesses reads the bearerAuth rights, the public marker
// and the 403 answer of every operation under /api/v1.
func documentedTokenAccesses(t *testing.T) map[string]documentedTokenAccess {
	t.Helper()
	accesses := make(map[string]documentedTokenAccess)
	currentPath := ""
	currentOperation := ""
	scanner := bufio.NewScanner(bytes.NewReader(api.OpenAPIDocument))
	for scanner.Scan() {
		line := scanner.Text()
		pathMatch := openAPIPathLine.FindStringSubmatch(line)
		if pathMatch != nil {
			currentPath = pathMatch[1]
			currentOperation = ""
			continue
		}
		if line != "" && !strings.HasPrefix(line, "    ") {
			currentPath = ""
			currentOperation = ""
			continue
		}
		methodMatch := openAPIMethodLine.FindStringSubmatch(line)
		if methodMatch != nil && strings.HasPrefix(currentPath, "/api/v1/") {
			currentOperation = strings.ToUpper(methodMatch[1]) + " " + currentPath
			accesses[currentOperation] = documentedTokenAccess{}
			continue
		}
		if currentOperation == "" {
			continue
		}
		access := accesses[currentOperation]
		rightsMatch := openAPIRights.FindStringSubmatch(line)
		switch {
		case rightsMatch != nil:
			access.rights = strings.ReplaceAll(rightsMatch[1], ", ", ",")
		case line == "      security: []":
			access.public = true
		case line == "        '403':":
			access.forbidden = true
		}
		accesses[currentOperation] = access
	}
	err := scanner.Err()
	if err != nil {
		t.Fatal(err)
	}
	return accesses
}

// TestOpenAPIDocumentsTheRights keeps the bearerAuth entries of the
// document in sync with the right of every route: an operation a
// connection may not call lists no bearerAuth, the others the rights the
// route names, and every operation that can miss a right documents the 403.
func TestOpenAPIDocumentsTheRights(t *testing.T) {
	environment := newAPIEnvironment(t)
	documented := documentedTokenAccesses(t)
	for _, route := range environment.routes {
		access, ok := documented[route.Pattern]
		if !ok {
			continue
		}
		if !access.public && access.rights != route.Right.String() {
			t.Errorf("%s: openapi.yaml bearerAuth [%s], route needs [%s]", route.Pattern, access.rights, route.Right.String())
		}
		if access.public && access.rights != "" {
			t.Errorf("%s: a public operation lists bearerAuth", route.Pattern)
		}
		if route.Right != model.RightRead && !access.forbidden {
			t.Errorf("%s: can answer 403 to a connection, but openapi.yaml documents no 403", route.Pattern)
		}
	}
}

// readChanges are the routes that change something with the read right
// alone: removing a connection or CI token only takes access away (another
// person's CI token needs servers in the handler).
var readChanges = map[string]bool{
	"DELETE /api/v1/account/connections/{id}": true,
	"DELETE /api/v1/ci-tokens/{id}":           true,
}

// dataReads are the reads of app data, as strong as secrets: they need
// env, not read.
var dataReads = map[string]bool{
	"GET /api/v1/apps/{app}/databases/rows": true,
}

// TestRightsFollowTheMethod checks that every GET route needs read and
// nothing more, but for app data, and every route that changes data a
// right beyond read.
func TestRightsFollowTheMethod(t *testing.T) {
	environment := newAPIEnvironment(t)
	for _, route := range environment.routes {
		method, _, _ := strings.Cut(route.Pattern, " ")
		if dataReads[route.Pattern] {
			if route.Right != model.RightEnv {
				t.Errorf("%s reads app data with %s, want env", route.Pattern, route.Right)
			}
			continue
		}
		if method == http.MethodGet && route.Right != api.NoToken && route.Right != model.RightRead {
			t.Errorf("%s needs %s for a read", route.Pattern, route.Right)
		}
		if method != http.MethodGet && route.Right == model.RightRead && !readChanges[route.Pattern] {
			t.Errorf("%s changes data with the read right", route.Pattern)
		}
	}
}

// TestOpenAPIIsValidYAML: generators and viewers read the document with
// strict parsers. It parses, and no plain value is a word a YAML 1.1
// parser reads as a boolean, like on and off: those are quoted.
func TestOpenAPIIsValidYAML(t *testing.T) {
	var document yaml.Node
	if err := yaml.Unmarshal(api.OpenAPIDocument, &document); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %v", err)
	}
	booleans := []string{"y", "n", "yes", "no", "on", "off"}
	var walk func(node *yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.ScalarNode && node.Style == 0 && slices.Contains(booleans, strings.ToLower(node.Value)) {
			t.Errorf("openapi.yaml line %d: quote %q, a YAML 1.1 parser reads it as a boolean", node.Line, node.Value)
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(&document)
}
