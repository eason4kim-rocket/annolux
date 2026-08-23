package publicapi

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestOpenAPIRoutesExactlyMatchRuntimeRoutes(t *testing.T) {
	data, err := os.ReadFile("../openapi/annolux.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	actual := make([]string, 0)
	for _, route := range (&Server{}).routeDefinitions() {
		actual = append(actual, route.method+" "+route.path)
	}
	documented := make([]string, 0)
	for path, pathItem := range document.Paths {
		for method := range pathItem {
			switch method {
			case "get", "post", "put", "patch", "delete":
				documented = append(documented, strings.ToUpper(method)+" "+path)
			}
		}
	}
	sort.Strings(actual)
	sort.Strings(documented)
	if !reflect.DeepEqual(documented, actual) {
		t.Fatalf("OpenAPI routes differ\ndocumented=%v\nruntime=%v", documented, actual)
	}
}
