// Command smapigen generates the Alexa Skill Management API (SMAPI) client used by Encore from the pinned Swagger 2.0 spec published in alexa/alexa-apis-for-nodejs.
//
// Pipeline: verify the spec checksum, copy x-operation-name into operationId, convert Swagger 2.0 to OpenAPI 3 with kin-openapi, rewrite "*/*" request bodies to application/json, then run oapi-codegen as a library restricted to the operations in operations.go.
//
// Usage:
//
//	smapigen -spec DIR/spec.json -sum DIR/spec.json.sha256 -out DIR [-package NAME]
package main
