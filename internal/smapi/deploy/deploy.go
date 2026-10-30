package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/skillpkg"
)

// onlyUnreported reports whether every failed locale failed by never reporting a build.
func onlyUnreported(locales map[string][]smapi.Problem) bool {
	for _, problems := range locales {
		if len(problems) != 1 || problems[0].Message != unreportedBuild {
			return false
		}
	}

	return true
}

// New returns a Deployer.
func New(client *smapi.Client, src *skillmodel.Sources) *Deployer {
	return &Deployer{client: client, src: src}
}

// Build builds the package a deploy with opts would upload, e.g. to compare its Hash with State.NeedsDeploy.
func (d *Deployer) Build(opts skillpkg.Options) (*skillpkg.Package, error) {
	return skillpkg.Build(d.src, opts)
}

// ExportPackage writes the package with an ask-resources.json, for deploying by hand with `ask deploy`.
func (d *Deployer) ExportPackage(w io.Writer, opts skillpkg.Options) error {
	pkg, err := d.Build(opts)
	if err != nil {
		return err
	}

	return pkg.Export(w)
}

// Resync rebuilds with new slot values (e.g. after a library scan) and deploys only if the package changed. Debouncing is the caller's job.
func (d *Deployer) Resync(ctx context.Context, req Request, slotValues map[string][]skillmodel.SlotValue) (*Result, error) {
	req.Package.SlotValues = slotValues
	req.Force = false

	return d.Deploy(ctx, req)
}

// Deploy runs a deploy.
func (d *Deployer) Deploy(ctx context.Context, req Request) (*Result, error) {
	r := &run{d: d, req: req, res: &Result{State: req.State, Locales: map[string]LocaleStatus{}}}

	err := r.deploy(ctx)
	if err != nil && r.step != "" {
		r.emit(Event{Step: r.step, State: StateFailed, Message: err.Error()})
	}

	return r.res, err
}

func (r *run) debug(msg string, args ...any) {
	if r.req.Log != nil {
		r.req.Log.Debug(msg, args...)
	}
}

func (r *run) emit(ev Event) {
	if r.req.Progress != nil {
		r.req.Progress(ev)
	}
}

func (r *run) start(step Step, msg string) {
	r.step = step
	r.emit(Event{Step: step, State: StateRunning, Message: msg})
}

func (r *run) done(msg string) {
	r.emit(Event{Step: r.step, State: StateDone, Message: msg})
	r.step = ""
}

func (r *run) skip(step Step, msg string) {
	r.emit(Event{Step: step, State: StateSkipped, Message: msg})
}

func (r *run) deploy(ctx context.Context) error {
	r.start(StepPackage, "Building the skill package")

	pkg, err := r.d.Build(r.req.Package)
	if err != nil {
		return err
	}

	r.pkg = pkg
	r.res.Warnings = append(r.res.Warnings, pkg.Warnings...)

	if !r.req.Force && !r.res.State.NeedsDeploy(pkg.Hash()) {
		r.res.Skipped = true
		r.res.Degraded = r.res.State.Degraded
		r.done("Unchanged since the last deploy")

		for _, s := range []Step{StepVendor, StepSkill, StepUpload, StepImport, StepBuild, StepEnable, StepSimulate} {
			r.skip(s, "Unchanged")
		}

		return nil
	}

	// Until this deploy succeeds, what is live no longer matches any known hash.
	r.res.State.Hash, r.res.State.Degraded = "", nil
	r.done(fmt.Sprintf("%d locales, hash %.12s", len(pkg.Locales()), pkg.Hash()))

	if err := r.resolveVendor(ctx); err != nil {
		return err
	}

	if err := r.resolveSkill(ctx); err != nil {
		return err
	}

	if err := r.ship(ctx); err != nil {
		de, ok := errors.AsType[*Error](err)
		if !ok || !r.degrade(de) {
			return err
		}

		if err := r.ship(ctx); err != nil {
			r.res.Degraded = nil

			return err
		}
	}

	r.start(StepEnable, "Enabling the skill for testing")

	if err := r.d.client.EnableDevelopment(ctx, r.res.State.SkillID); err != nil {
		return err
	}

	r.done("Enabled on devices registered to your Amazon account")

	if err := r.simulate(ctx); err != nil {
		return err
	}

	// A package deployed with fewer slot values still counts as this one, so it isn't deployed again until something changes.
	r.res.State.Hash, r.res.State.Degraded = pkg.Hash(), r.res.Degraded

	return nil
}

// ship uploads the package, imports it and waits for its models to build.
func (r *run) ship(ctx context.Context) error {
	location, err := r.upload(ctx)
	if err != nil {
		return err
	}

	if err := r.importPackage(ctx, location); err != nil {
		return err
	}

	return r.waitForBuild(ctx)
}

// degrade prepares a second attempt, once a deploy, after Amazon would not build some locales' models for reasons it named. A locale whose errors quote some of its slot values is built without them; otherwise, when other locales did build, it keeps every other value of each slot type but Request.KeepSlots. Locales that failed without a reason are left as they are, as their failure may pass. It reports false when nothing would change, and the failure stands.
func (r *run) degrade(de *Error) bool {
	if r.retried || len(de.Locales) == 0 {
		return false
	}

	partial := len(de.Locales) < len(r.pkg.Locales())
	values := r.req.Package.SlotValues
	total := countValues(values)
	overrides := map[string]map[string][]skillmodel.SlotValue{}
	degraded := map[string]Degradation{}

	for _, loc := range slices.Sorted(maps.Keys(de.Locales)) {
		problems := de.Locales[loc]
		if (&Error{Locales: map[string][]smapi.Problem{loc: problems}}).unexplained() {
			continue
		}

		own, dropped := withoutNamed(values, r.pkg.Models[loc], problems)
		if len(dropped) == 0 {
			if !partial {
				continue
			}

			own = thinned(values, r.req.KeepSlots)
		}

		if kept := countValues(own); kept < total {
			overrides[loc] = own
			degraded[loc] = Degradation{Reason: problems, Dropped: dropped, Kept: kept, Total: total}
		}
	}

	if len(overrides) == 0 {
		return false
	}

	opts := r.req.Package
	opts.LocaleSlotValues = overrides

	pkg, err := r.d.Build(opts)
	if err != nil {
		return false
	}

	for _, f := range pkg.Files {
		loc := strings.TrimSuffix(strings.TrimPrefix(f.Name, skillpkg.ModelDir), ".json")
		if d, ok := degraded[loc]; ok && strings.HasPrefix(f.Name, skillpkg.ModelDir) {
			d.Bytes = len(f.Data)
			degraded[loc] = d
		}
	}

	locales := slices.Sorted(maps.Keys(overrides))
	r.emit(Event{Step: StepBuild, State: StateFailed, Message: "Amazon could not build " + strings.Join(locales, ", ") + "; deploying again with fewer library titles there", Locales: r.snapshot()})
	r.step = ""

	r.retried = true
	r.pkg = pkg
	r.res.Degraded = degraded
	r.res.Locales = map[string]LocaleStatus{}
	r.imported, r.missing = nil, nil

	return true
}

func countValues(values map[string][]skillmodel.SlotValue) int {
	n := 0
	for _, vs := range values {
		n += len(vs)
	}

	return n
}

// withoutNamed leaves out the values that problems quote, by value or synonym, ignoring case, and returns them. Amazon quotes built, the model it was given, whose values are cleaned for it (skillpkg.CleanSlotText) and may carry synonyms the build added, such as native-script forms, so a value is also left out when its cleaned text is that of a value built quotes.
func withoutNamed(values map[string][]skillmodel.SlotValue, built *skillmodel.InteractionModel, problems []smapi.Problem) (map[string][]skillmodel.SlotValue, []string) {
	var text strings.Builder

	for _, p := range problems {
		text.WriteString(strings.ToLower(p.Message))
		text.WriteByte('\n')
	}

	msg := text.String()
	quoted := func(s string) bool {
		s = strings.ToLower(s)
		for _, q := range valueQuotes {
			if strings.Contains(msg, q[0]+s+q[1]) {
				return true
			}
		}

		return false
	}

	named := map[string]map[string]bool{}

	if built != nil {
		for _, typ := range built.InteractionModel.LanguageModel.Types {
			for _, v := range typ.Values {
				if quoted(v.Name.Value) || slices.ContainsFunc(v.Name.Synonyms, quoted) {
					if named[typ.Name] == nil {
						named[typ.Name] = map[string]bool{}
					}

					named[typ.Name][strings.ToLower(v.Name.Value)] = true
				}
			}
		}
	}

	out := make(map[string][]skillmodel.SlotValue, len(values))

	var dropped []string

	for _, typ := range slices.Sorted(maps.Keys(values)) {
		kept := make([]skillmodel.SlotValue, 0, len(values[typ]))

		for _, v := range values[typ] {
			if v.Value != "" && (quoted(v.Value) || slices.ContainsFunc(v.Synonyms, quoted) || named[typ][strings.ToLower(skillpkg.CleanSlotText(v.Value))]) {
				dropped = append(dropped, v.Value)

				continue
			}

			kept = append(kept, v)
		}

		out[typ] = kept
	}

	return out, dropped
}

// thinned keeps every other value of each slot type but keep, starting with the second, so a type of one value loses it.
func thinned(values map[string][]skillmodel.SlotValue, keep []string) map[string][]skillmodel.SlotValue {
	out := make(map[string][]skillmodel.SlotValue, len(values))

	for typ, vs := range values {
		if slices.Contains(keep, typ) {
			out[typ] = vs

			continue
		}

		half := make([]skillmodel.SlotValue, 0, len(vs)/2)
		for i := 1; i < len(vs); i += 2 {
			half = append(half, vs[i])
		}

		out[typ] = half
	}

	return out
}

func (r *run) resolveVendor(ctx context.Context) error {
	if r.res.State.VendorID != "" {
		r.skip(StepVendor, "Using vendor "+r.res.State.VendorID)

		return nil
	}

	r.start(StepVendor, "Finding your developer account")

	vendors, err := r.d.client.Vendors(ctx)
	if err != nil {
		return err
	}

	if len(vendors) != 1 {
		return &VendorChoiceError{Vendors: vendors}
	}

	r.res.State.VendorID = vendors[0].ID
	r.done("Using " + vendors[0].Name)

	return nil
}

// resolveSkill finds the skill by name when the stored skill ID was lost, so a reinstall does not create a duplicate.
func (r *run) resolveSkill(ctx context.Context) error {
	if r.res.State.SkillID != "" {
		r.skip(StepSkill, "Updating skill "+r.res.State.SkillID)

		return nil
	}

	r.start(StepSkill, "Looking for an existing Encore skill")

	skills, err := r.d.client.ListSkills(ctx, r.res.State.VendorID)
	if err != nil {
		return err
	}

	names := r.pkg.Manifest.Manifest.PublishingInformation.Locales

	for _, s := range skills {
		if s.Stage != "" && s.Stage != smapi.StageDevelopment {
			continue
		}

		if len(s.APIs) > 0 && !slices.Contains(s.APIs, "custom") {
			continue
		}

		for loc, name := range s.NameByLocale {
			if lp, ok := names[loc]; ok && lp.Name == name {
				r.res.State.SkillID = s.ID
				r.done("Found " + s.ID)

				return nil
			}
		}
	}

	r.done("None found; a new skill will be created")

	return nil
}

func (r *run) upload(ctx context.Context) (string, error) {
	r.start(StepUpload, "Uploading the skill package")

	data, err := r.pkg.Zip()
	if err != nil {
		return "", err
	}

	up, err := r.d.client.CreateUpload(ctx)
	if err != nil {
		return "", err
	}

	if err := r.d.client.PutPackage(ctx, up.URL, data); err != nil {
		return "", err
	}

	r.done(fmt.Sprintf("%d bytes", len(data)))

	return up.URL, nil
}

func (r *run) importPackage(ctx context.Context, location string) error {
	r.start(StepImport, "Importing the skill package")

	var (
		importID string
		err      error
	)

	if r.res.State.SkillID != "" {
		importID, err = r.d.client.ImportExisting(ctx, r.res.State.SkillID, location, "")
		if smapi.StatusCode(err) == http.StatusNotFound {
			// The skill was deleted in the developer console; start afresh.
			r.res.State.SkillID = ""
			r.res.Warnings = append(r.res.Warnings, "the previously deployed skill no longer exists; created a new one")
			importID, err = r.d.client.ImportNew(ctx, r.res.State.VendorID, location)
		}
	} else {
		importID, err = r.d.client.ImportNew(ctx, r.res.State.VendorID, location)
	}

	if err != nil {
		return err
	}

	var status *smapi.ImportStatus

	err = r.d.client.Poll(ctx, func(ctx context.Context) (bool, error) {
		status, err = r.d.client.ImportStatus(ctx, importID)
		if err != nil {
			return false, err
		}

		r.debug("skill import status", "import", importID, "status", status.Status, "resources", resourceSummary(status.Skill.Resources))

		if status.Skill.ID != "" {
			r.res.State.SkillID = status.Skill.ID
		}

		return status.Status != smapi.StatusInProgress, nil
	})
	if err != nil {
		return err
	}

	for _, w := range status.Warnings {
		r.res.Warnings = append(r.res.Warnings, w.String())
	}

	if status.Status == smapi.StatusSucceeded {
		r.recordImported(status.Skill.Resources)
		r.done("Imported into " + r.res.State.SkillID)

		return nil
	}

	failure := &Error{Step: StepImport, Problems: status.Errors}

	// Resources report their own errors whether they failed outright or were rolled back.
	for _, res := range status.Skill.Resources {
		failure.Problems = append(failure.Problems, res.Errors...)
	}

	// A failed import is usually a failed model build; the skill status has the per-locale details. A locale whose import was rolled back may never report a build; pollBuild gives up on those, and then the import's own errors are the ones to report.
	if r.res.State.SkillID != "" {
		if err := r.pollBuild(ctx, false); err == nil {
			if locales := r.failedLocales(); len(locales) > 0 && !onlyUnreported(locales) {
				r.emit(Event{Step: StepImport, State: StateFailed, Message: "Amazon could not build the interaction models"})
				r.step = StepBuild

				return &Error{Step: StepBuild, Locales: locales}
			}
		}
	}

	return failure
}

func (r *run) waitForBuild(ctx context.Context) error {
	r.start(StepBuild, "Waiting for the interaction models to build")

	// The import builds the models; when it reported every locale built there is nothing left to wait for.
	if !slices.ContainsFunc(r.pkg.Locales(), func(loc string) bool { return !r.imported[loc] }) {
		r.emit(Event{Step: StepBuild, State: StateDone, Message: "Built during the import", Locales: r.snapshot()})
		r.step = ""

		return nil
	}

	if err := r.pollBuild(ctx, true); err != nil {
		return err
	}

	if locales := r.failedLocales(); len(locales) > 0 {
		return &Error{Step: StepBuild, Locales: locales}
	}

	r.emit(Event{Step: StepBuild, State: StateDone, Locales: r.snapshot()})
	r.step = ""

	return nil
}

func (r *run) pollBuild(ctx context.Context, progress bool) error {
	return r.d.client.Poll(ctx, func(ctx context.Context) (bool, error) {
		st, err := r.d.client.SkillStatus(ctx, r.res.State.SkillID)
		if err != nil {
			return false, err
		}

		finished := r.recordLocales(st)
		manifestDone := st.Manifest == nil || st.Manifest.LastUpdateRequest.Status != smapi.StatusInProgress

		r.debug("skill build status", "skill", r.res.State.SkillID, "manifest_done", manifestDone, "locales", r.snapshot(), "raw", string(st.Raw))

		// A locale Amazon still hasn't reported long after the manifest finished isn't going to build.
		if manifestDone {
			for _, loc := range r.pkg.Locales() {
				if r.missing[loc] >= missingLocalePolls {
					r.res.Locales[loc] = LocaleStatus{Status: smapi.StatusFailed, Errors: []smapi.Problem{{Message: unreportedBuild}}}
					finished = true
				}
			}
		}

		if progress {
			r.emit(Event{Step: StepBuild, State: StateRunning, Locales: r.snapshot()})
		}

		if finished {
			finished = !slices.ContainsFunc(r.pkg.Locales(), func(loc string) bool { return r.res.Locales[loc].Status == smapi.StatusInProgress })
		}

		return finished && manifestDone, nil
	})
}

// recordLocales reports whether every package locale has finished. A locale missing from the status has not started.
func (r *run) recordLocales(st *smapi.SkillStatus) bool {
	finished := true

	for _, loc := range r.pkg.Locales() {
		if r.imported[loc] {
			continue
		}

		ls := LocaleStatus{Status: smapi.StatusInProgress}

		// Amazon names locales both ways: en-AU in its documents, en_AU in resource names and the console.
		m, ok := st.InteractionModel[loc]
		if !ok {
			m, ok = st.InteractionModel[strings.ReplaceAll(loc, "-", "_")]
		}

		if ok && m.LastUpdateRequest.Status != "" {
			b := m.LastUpdateRequest
			ls = LocaleStatus{Status: b.Status, Errors: b.Problems(), Warnings: b.Warnings}

			if r.missing != nil {
				delete(r.missing, loc)
			}
		} else {
			if r.missing == nil {
				r.missing = map[string]int{}
			}

			r.missing[loc]++
		}

		// A locale already marked failed for not reporting stays failed.
		if prev, seen := r.res.Locales[loc]; seen && prev.Status == smapi.StatusFailed && !ok {
			ls = prev
		}

		if ls.Status == smapi.StatusInProgress {
			finished = false
		}

		r.res.Locales[loc] = ls
	}

	return finished
}

func (r *run) failedLocales() map[string][]smapi.Problem {
	out := map[string][]smapi.Problem{}

	for loc, ls := range r.res.Locales {
		if ls.Status == smapi.StatusFailed {
			errs := ls.Errors
			if len(errs) == 0 {
				errs = []smapi.Problem{{Message: unexplainedBuild}}
			}

			out[loc] = errs
		}
	}

	return out
}

func (r *run) snapshot() map[string]LocaleStatus {
	return maps.Clone(r.res.Locales)
}

func resourceSummary(resources []smapi.ResourceStatus) []string {
	out := make([]string, 0, len(resources))
	for _, r := range resources {
		out = append(out, r.Name+"="+string(r.Status))
	}

	return out
}

// Amazon names import resources "InteractionModel.en-AU" or "interactionModels.en_AU".
func (r *run) recordImported(resources []smapi.ResourceStatus) {
	for _, res := range resources {
		kind, loc, ok := strings.Cut(res.Name, ".")
		// SKIPPED means the model didn't change, so the one Amazon already built stands.
		built := res.Status == smapi.StatusSucceeded || res.Status == smapi.StatusSkipped
		if !ok || !strings.HasPrefix(strings.ToLower(kind), "interactionmodel") || !built {
			continue
		}

		loc = strings.ReplaceAll(loc, "_", "-")
		if !slices.Contains(r.pkg.Locales(), loc) {
			continue
		}

		if r.imported == nil {
			r.imported = map[string]bool{}
		}

		r.imported[loc] = true
		r.res.Locales[loc] = LocaleStatus{Status: smapi.StatusSucceeded, Warnings: res.Warnings}
	}
}
