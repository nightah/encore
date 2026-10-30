package main

// operations is the allow-list of SMAPI operations (by x-operation-name) that Encore uses. Only these operations, and the schemas they reference, are generated. Adding an operation here and re-running go generate is all that is needed to expose another endpoint.
var operations = []string{
	// Vendors.
	"getVendorListV1", // GET /v1/vendors

	// Skills.
	"listSkillsForVendorV1", // GET /v1/skills
	"getSkillStatusV1",      // GET /v1/skills/{skillId}/status
	"getSkillManifestV1",    // GET /v1/skills/{skillId}/stages/{stage}/manifest

	// Skill packages, which create and update the skill.
	"createUploadUrlV1",    // POST /v1/skills/uploads
	"createSkillPackageV1", // POST /v1/skills/imports (new skill)
	"importSkillPackageV1", // POST /v1/skills/{skillId}/imports (existing skill)
	"getImportStatusV1",    // GET /v1/skills/imports/{importId}

	// Enablement.
	"setSkillEnablementV1", // PUT /v1/skills/{skillId}/stages/{stage}/enablement

	// Interaction model evaluation.
	"profileNluV1", // POST /v1/skills/{skillId}/stages/{stage}/interactionModel/locales/{locale}/profileNlu

	// Simulation.
	"simulateSkillV2",      // POST /v2/skills/{skillId}/stages/{stage}/simulations
	"getSkillSimulationV2", // GET /v2/skills/{skillId}/stages/{stage}/simulations/{simulationId}
}
