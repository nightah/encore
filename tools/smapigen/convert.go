package main

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
)

// httpMethods are the Swagger 2.0 path item keys that hold operations.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch"}

// assignOperationIDs copies each operation's x-operation-name into operationId (the spec has none), suffixing duplicates in path order so the result is deterministic.
func assignOperationIDs(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("spec has no paths")
	}

	seen := map[string]int{}
	for _, path := range sortedKeys(paths) {
		item, _ := paths[path].(map[string]any)
		for _, method := range httpMethods {
			op, ok := item[method].(map[string]any)
			if !ok {
				continue
			}
			name, _ := op["x-operation-name"].(string)
			if name == "" {
				return nil, fmt.Errorf("%s %s has no x-operation-name", method, path)
			}
			seen[name]++
			if n := seen[name]; n > 1 {
				name = fmt.Sprintf("%s%d", name, n)
			}
			op["operationId"] = name
		}
	}
	return json.Marshal(doc)
}

func toOpenAPI3(raw []byte) (*openapi3.T, error) {
	var v2 openapi2.T
	if err := json.Unmarshal(raw, &v2); err != nil {
		return nil, fmt.Errorf("decode swagger 2.0: %w", err)
	}
	v3, err := openapi2conv.ToV3(&v2)
	if err != nil {
		return nil, fmt.Errorf("convert to openapi 3: %w", err)
	}

	// Reload through JSON: the converter leaves empty, non-nil slices (such as allOf) that oapi-codegen treats as present, and loading resolves every $ref the same way the oapi-codegen CLI would.
	b, err := json.Marshal(v3)
	if err != nil {
		return nil, err
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(b)
	if err != nil {
		return nil, fmt.Errorf("reload openapi 3: %w", err)
	}
	return doc, nil
}

// fixRequestBodies rewrites "*/*" request body media types, which the converter emits for operations without "consumes", to application/json so oapi-codegen generates typed JSON body helpers.
func fixRequestBodies(doc *openapi3.T) {
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.RequestBody == nil || op.RequestBody.Value == nil {
				continue
			}
			content := op.RequestBody.Value.Content
			if mt, ok := content["*/*"]; ok {
				if _, exists := content["application/json"]; !exists {
					content["application/json"] = mt
				}
				delete(content, "*/*")
			}
		}
	}
}

// checkOperations reports allow-listed operations missing from doc.
func checkOperations(doc *openapi3.T, allow []string) error {
	var ids []string
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			ids = append(ids, op.OperationID)
		}
	}
	for _, id := range allow {
		if !slices.Contains(ids, id) {
			return fmt.Errorf("allow-listed operation %q is not in the spec", id)
		}
	}
	return nil
}
