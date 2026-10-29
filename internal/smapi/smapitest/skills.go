package smapitest

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleVendors(w http.ResponseWriter, _ *http.Request) {
	vendors := make([]map[string]any, 0, len(s.opts.Vendors))
	for _, v := range s.opts.Vendors {
		vendors = append(vendors, map[string]any{"id": v.ID, "name": v.Name, "roles": v.Roles})
	}

	writeJSON(w, http.StatusOK, map[string]any{"vendors": vendors})
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	vendorID := q.Get("vendorId")
	if vendorID == "" {
		writeError(w, http.StatusBadRequest, "vendorId is required")

		return
	}

	pageSize := 10
	if n, err := strconv.Atoi(q.Get("maxResults")); err == nil && n > 0 {
		pageSize = n
	}

	start, _ := strconv.Atoi(q.Get("nextToken"))

	s.mu.Lock()
	defer s.mu.Unlock()

	var all []*Skill

	for _, id := range s.order {
		if sk := s.skills[id]; sk.VendorID == vendorID {
			all = append(all, sk)
		}
	}

	end := min(start+pageSize, len(all))
	skills := []map[string]any{}

	for _, sk := range all[min(start, end):end] {
		skills = append(skills, map[string]any{
			"skillId":      sk.ID,
			"nameByLocale": sk.Names,
			"stage":        "development",
			"apis":         []string{"custom"},
			"lastUpdated":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	resp := map[string]any{"skills": skills, "isTruncated": end < len(all)}
	if end < len(all) {
		resp["nextToken"] = strconv.Itoa(end)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateUpload(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	id := s.nextID("upload-")
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{
		"uploadUrl": s.URL + "/s3/" + id + "?X-Amz-Signature=fake",
		// The live API sends Unix milliseconds as a string here, not the RFC 3339 its spec documents.
		"expiresAt": strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10),
	})
}

func (s *Server) handleS3Put(w http.ResponseWriter, r *http.Request) {
	// A presigned URL already carries its authorisation; S3 rejects a second one.
	if r.Header.Get("Authorization") != "" {
		http.Error(w, "<Error><Code>InvalidArgument</Code></Error>", http.StatusBadRequest)

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	s.mu.Lock()
	s.uploads[r.PathValue("id")] = body
	s.calls["PUT /s3/{id}"]++
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleImportNew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VendorID string `json:"vendorId"`
		Location string `json:"location"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Location == "" || req.VendorID == "" {
		writeError(w, http.StatusBadRequest, "vendorId and location are required")

		return
	}

	s.startImport(w, "", req.VendorID, req.Location)
}

func (s *Server) handleImportExisting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Location string `json:"location"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Location == "" {
		writeError(w, http.StatusBadRequest, "location is required")

		return
	}

	s.startImport(w, r.PathValue("skillId"), "", req.Location)
}

func (s *Server) startImport(w http.ResponseWriter, skillID, vendorID, location string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, ok := s.uploads[uploadID(location, s.URL)]
	if !ok {
		writeError(w, http.StatusBadRequest, "location %s was not uploaded", location)

		return
	}

	var sk *Skill

	if skillID == "" {
		sk = &Skill{ID: s.nextID("amzn1.ask.skill."), VendorID: vendorID, Models: map[string]json.RawMessage{}}
		s.addSkill(sk)
	} else if sk, ok = s.skills[skillID]; !ok {
		writeError(w, http.StatusNotFound, "skill %s not found", skillID)

		return
	}

	job := &importJob{id: s.nextID("import-"), skillID: sk.ID, failed: map[string]string{}, skipped: map[string]bool{}}

	pkg, err := parsePackage(data)
	if err != nil {
		job.errs = append(job.errs, problem{Code: "INVALID_PACKAGE", Message: err.Error()})
	} else {
		for loc, model := range pkg.models {
			if prev, ok := sk.Models[loc]; ok && bytes.Equal(prev, model) {
				job.skipped[loc] = true
			}
		}

		sk.Names, sk.Manifest, sk.Models = pkg.names, pkg.manifest, pkg.models
		sk.Imports++
		job.locales = slices.Sorted(maps.Keys(pkg.models))

		for _, loc := range job.locales {
			if msg, bad := s.failLocales[loc]; bad {
				job.failed[loc] = msg
			}
		}
	}

	s.imports[job.id] = job
	s.lastImport[sk.ID] = job
	s.statusPolls[sk.ID] = 0

	w.Header().Set("Location", "/v1/skills/imports/"+job.id)
	w.WriteHeader(http.StatusAccepted)
}

func uploadID(location, base string) string {
	rest, ok := strings.CutPrefix(location, base+"/s3/")
	if !ok {
		return ""
	}

	id, _, _ := strings.Cut(rest, "?")

	return id
}

func (s *Server) handleSkillGet(w http.ResponseWriter, r *http.Request) {
	a, b := r.PathValue("a"), r.PathValue("b")

	switch {
	case a == "imports":
		s.handleImportStatus(w, b)
	case b == "status":
		s.handleSkillStatus(w, r, a)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleImportStatus(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.imports[id]
	if !ok {
		writeError(w, http.StatusNotFound, "import %s not found", id)

		return
	}

	resp := map[string]any{"skill": map[string]any{"skillId": job.skillID, "resources": []any{}}}

	if job.polls < s.opts.Pending {
		job.polls++
		resp["status"] = "IN_PROGRESS"
		writeJSON(w, http.StatusOK, resp)

		return
	}

	status := "SUCCEEDED"
	resources := []map[string]any{{"name": "Manifest", "action": "UPDATE", "status": "SUCCEEDED"}}

	for _, loc := range job.locales {
		_, bad := job.failed[loc]
		if (s.opts.ImportWithoutModels || s.unimported[loc]) && !bad {
			continue
		}

		res := map[string]any{"name": "InteractionModel." + s.localeKey(loc), "action": "UPDATE", "status": "SUCCEEDED"}
		if job.skipped[loc] && !bad {
			res["status"] = "SKIPPED"
		}

		if msg, bad := job.failed[loc]; bad {
			res["status"] = "FAILED"
			res["errors"] = []problem{{Code: "INVALID_INTERACTION_MODEL", Message: msg}}
			status = "FAILED"
		}

		resources = append(resources, res)
	}

	if len(job.errs) > 0 {
		status = "FAILED"
		resp["errors"] = job.errs
	}

	resp["status"] = status
	resp["skill"] = map[string]any{"skillId": job.skillID, "eTag": "etag-" + job.id, "resources": resources}

	writeJSON(w, http.StatusOK, resp)
}

// handleSkillStatus answers like the live API: interaction model builds only when asked for with ?resource=interactionModel (unfiltered, it reports just the manifest), and the manifest unless another resource was asked for.
func (s *Server) handleSkillStatus(w http.ResponseWriter, r *http.Request, skillID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sk, ok := s.skills[skillID]
	if !ok {
		writeError(w, http.StatusNotFound, "skill %s not found", skillID)

		return
	}

	failed := map[string]string{}
	if job, ok := s.lastImport[skillID]; ok {
		failed = job.failed
	}

	pending := s.statusPolls[skillID] < s.opts.Pending
	s.statusPolls[skillID]++

	models := map[string]any{}

	for loc := range sk.Models {
		if s.unreported[loc] {
			continue
		}

		lur := map[string]any{"status": "SUCCEEDED"}

		switch msg, bad := failed[loc]; {
		case pending:
			lur["status"] = "IN_PROGRESS"
		case bad:
			errs := []problem{{Code: "INVALID_INTERACTION_MODEL", Message: msg}}
			lur["status"] = "FAILED"
			lur["errors"] = errs
			lur["buildDetails"] = map[string]any{"steps": []any{
				map[string]any{"name": "LANGUAGE_MODEL_QUICK_BUILD", "status": "FAILED", "errors": errs},
			}}
		default:
			lur["buildDetails"] = map[string]any{"steps": []any{
				map[string]any{"name": "LANGUAGE_MODEL_QUICK_BUILD", "status": "SUCCEEDED"},
				map[string]any{"name": "LANGUAGE_MODEL_FULL_BUILD", "status": "SUCCEEDED"},
			}}
		}

		models[s.localeKey(loc)] = map[string]any{"eTag": "model-etag", "lastUpdateRequest": lur}
	}

	manifest := "SUCCEEDED"
	if pending {
		manifest = "IN_PROGRESS"
	}

	out := map[string]any{}

	switch r.URL.Query().Get("resource") {
	case "":
		out["manifest"] = map[string]any{"eTag": "manifest-etag", "lastUpdateRequest": map[string]any{"status": manifest}}
	case "manifest":
		out["manifest"] = map[string]any{"eTag": "manifest-etag", "lastUpdateRequest": map[string]any{"status": manifest}}
	case "interactionModel":
		out["interactionModel"] = models
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleEnable(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sk, ok := s.skills[r.PathValue("skillId")]
	if !ok {
		writeError(w, http.StatusNotFound, "skill not found")

		return
	}

	if r.PathValue("stage") != "development" {
		writeError(w, http.StatusBadRequest, "only the development stage can be enabled")

		return
	}

	sk.Enabled = true
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetEnablement(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sk, ok := s.skills[r.PathValue("skillId")]
	if !ok || !sk.Enabled {
		writeError(w, http.StatusNotFound, "enablement not found")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
