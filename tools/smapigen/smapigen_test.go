package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestVerifyChecksum(t *testing.T) {
	data := []byte(`{"swagger": "2.0"}`)
	sum := sha256.Sum256(data)
	dir := t.TempDir()

	good := filepath.Join(dir, "good.sha256")
	if err := os.WriteFile(good, []byte(hex.EncodeToString(sum[:])+"  spec.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(data, good); err != nil {
		t.Errorf("matching checksum rejected: %v", err)
	}

	bad := filepath.Join(dir, "bad.sha256")
	if err := os.WriteFile(bad, []byte(strings.Repeat("0", 64)+"  spec.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(data, bad); err == nil {
		t.Error("mismatching checksum accepted")
	}
}

func TestAssignOperationIDs(t *testing.T) {
	raw := []byte(`{"swagger": "2.0", "paths": {
		"/b": {"get": {"x-operation-name": "dup"}},
		"/a": {"post": {"x-operation-name": "dup"}, "get": {"x-operation-name": "getA"}, "parameters": []}
	}}`)
	out, err := assignOperationIDs(raw)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	// Paths are visited in sorted order, methods in httpMethods order.
	want := map[string]string{"/a get": "getA", "/a post": "dup", "/b get": "dup2"}
	for key, id := range want {
		path, method, _ := strings.Cut(key, " ")
		var op struct {
			OperationID string `json:"operationId"`
		}
		if err := json.Unmarshal(doc.Paths[path][method], &op); err != nil {
			t.Fatal(err)
		}
		if got := op.OperationID; got != id {
			t.Errorf("%s operationId = %q, want %q", key, got, id)
		}
	}

	if _, err := assignOperationIDs([]byte(`{"paths": {"/x": {"get": {}}}}`)); err == nil {
		t.Error("expected an error for an operation without x-operation-name")
	}
}

func TestFixRequestBodies(t *testing.T) {
	doc, err := toOpenAPI3([]byte(`{"swagger": "2.0", "info": {"title": "t", "version": "1"}, "paths": {
		"/x": {"post": {"operationId": "postX",
			"parameters": [{"in": "body", "name": "body", "schema": {"type": "object"}}],
			"responses": {"200": {"description": "ok"}}}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	fixRequestBodies(doc)
	content := doc.Paths.Find("/x").Post.RequestBody.Value.Content
	if _, ok := content["*/*"]; ok {
		t.Error("*/* media type not removed")
	}
	if _, ok := content["application/json"]; !ok {
		t.Error("application/json media type not added")
	}
}

func TestCheckOperations(t *testing.T) {
	doc := &openapi3.T{Paths: openapi3.NewPaths(openapi3.WithPath("/x", &openapi3.PathItem{
		Get: &openapi3.Operation{OperationID: "getX"},
	}))}
	if err := checkOperations(doc, []string{"getX"}); err != nil {
		t.Error(err)
	}
	if err := checkOperations(doc, []string{"getY"}); err == nil {
		t.Error("expected an error for a missing operation")
	}
}
