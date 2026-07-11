// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"time"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/tgfeed/internal/admin/components"

	"github.com/a-h/templ"
)

type ui struct {
	api            *api
	staticHashName func(context.Context, string) string
}

func newUI(api *api, staticHashName func(context.Context, string) string) *ui {
	return &ui{
		api:            api,
		staticHashName: staticHashName,
	}
}

func (u *ui) handleStats(w http.ResponseWriter, r *http.Request) {
	p := u.statsPage(r)
	u.render(w, r, p, components.FragmentDashboardContent, components.FragmentStatsContent)
}

func (u *ui) handleStatsNothingToSave(w http.ResponseWriter, r *http.Request) {
	p := u.statsPage(r)
	p.Banner = "Nothing to save"
	u.render(w, r, p, components.FragmentDashboardContent)
}

func (u *ui) statsPage(r *http.Request) components.PageProps {
	p := u.page("Stats", components.RouteStats)
	query, err := parseStatsQuery(r)
	if err != nil {
		p.Stats = &components.StatsProps{
			Error:       err.Error(),
			RefreshedAt: time.Now(),
		}
		return p
	}
	p.Stats = u.loadStats(r.Context(), query)
	return p
}

type statsQuery struct {
	SelectedAt  *int64
	AutoRefresh bool
	DetailsOpen bool
}

func parseStatsQuery(r *http.Request) (statsQuery, error) {
	autoRefresh, err := queryBool(r, "auto_refresh", false)
	if err != nil {
		return statsQuery{}, err
	}
	detailsOpen, err := queryBool(r, "details", false)
	if err != nil {
		return statsQuery{}, err
	}
	selectedAt, err := queryInt64Optional(r, "started_at_unix")
	if err != nil {
		return statsQuery{}, err
	}
	return statsQuery{
		SelectedAt:  selectedAt,
		AutoRefresh: autoRefresh,
		DetailsOpen: detailsOpen,
	}, nil
}

func (u *ui) loadStats(ctx context.Context, query statsQuery) *components.StatsProps {
	props := &components.StatsProps{
		AutoRefresh: query.AutoRefresh,
		DetailsOpen: query.DetailsOpen,
		RefreshedAt: time.Now(),
	}
	// Summaries provide the history needed by overview cards and charts without
	// decoding every stored run. Only the active run is loaded in full below.
	runs, err := u.api.statsStore.ListRunSummaries(ctx, 100, nil)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			props.Error = fmt.Sprintf("failed to read stats: %v", err)
		}
		return props
	}
	props.Runs = runs
	var startedAt int64
	if query.SelectedAt != nil {
		startedAt = *query.SelectedAt
		props.SelectedAt = startedAt
	} else if len(runs) > 0 {
		startedAt = runs[0].StartedAtUnix
	}
	if startedAt != 0 {
		loaded, loadErr := u.api.statsStore.LoadRunByStartedAt(ctx, startedAt)
		if loadErr == nil {
			props.Active = loaded
		} else if errors.Is(loadErr, sql.ErrNoRows) || errors.Is(loadErr, fs.ErrNotExist) {
			if query.SelectedAt != nil {
				err = fmt.Errorf("selected run %d is no longer available", startedAt)
			}
		} else {
			err = loadErr
		}
	}
	if err != nil {
		props.Error = fmt.Sprintf("failed to read stats: %v", err)
	}
	return props
}

func (u *ui) handleConfiguration(w http.ResponseWriter, r *http.Request) {
	u.render(w, r, u.configurationPage(r, ""),
		components.FragmentDashboardContent,
		components.FragmentConfigPanel,
		components.FragmentErrorPanel,
	)
}

func (u *ui) configurationPage(r *http.Request, banner string) components.PageProps {
	p := u.page("Configuration", components.RouteConfiguration)
	p.Banner = banner
	p.Configuration = new(components.ConfigurationProps)
	for _, resource := range u.editorResources() {
		value, err := resource.load(r.Context())
		*resource.Props(p.Configuration) = resource.NewProps(value, value, errorString(err))
	}
	return p
}

func (u *ui) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	u.handleSaveEditor(w, r, configResource(u.api))
}

func (u *ui) handleSaveErrorTemplate(w http.ResponseWriter, r *http.Request) {
	u.handleSaveEditor(w, r, errorTemplateResource(u.api))
}

func (u *ui) handleSaveEditor(w http.ResponseWriter, r *http.Request, resource editorResource) {
	value, ok := formValue(w, r, resource.Name)
	if !ok {
		return
	}
	p := u.configurationPage(r, "")
	props := resource.Props(p.Configuration)
	// Keep the persisted value as the baseline when saving fails. The submitted
	// value is rendered back to the user and remains visibly unsaved.
	baseline := props.Baseline
	err := errorFromProps(*props)
	if err == nil {
		err = resource.Save(r.Context(), value)
		if err == nil {
			baseline = value
		}
	}
	*props = resource.NewProps(value, baseline, errorString(err))
	u.render(w, r, p, resource.Fragment)
}

func (u *ui) handleSaveAll(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		web.RespondError(w, r, fmt.Errorf("%w: invalid form payload", web.ErrBadRequest))
		return
	}
	p := u.configurationPage(r, "Nothing to save")
	// Resources are saved independently so one invalid editor does not discard
	// a valid change in the other editor. The banner summarizes the aggregate.
	jobs, successes := 0, 0
	for _, resource := range u.editorResources() {
		values := r.Form[resource.Name]
		if len(values) == 0 {
			continue
		}
		value := values[0]
		props := resource.Props(p.Configuration)
		baseline := props.Baseline
		if value != baseline {
			jobs++
			err := resource.Save(r.Context(), value)
			if err == nil {
				successes++
				baseline = value
			}
			*props = resource.NewProps(value, baseline, errorString(err))
		}
	}
	if jobs > 0 && successes == jobs {
		p.Banner = "All changes saved"
	} else if jobs > 0 {
		p.Banner = "Some changes failed to save"
	}
	u.render(w, r, p, components.FragmentDashboardContent)
}

func (u *ui) page(title string, route components.Route) components.PageProps {
	return components.PageProps{
		Title: title,
		Route: route,
		JS:    "static/js/app.min.js",
		CSS:   "static/css/app.min.css",
		Icon:  "static/icons/icon.webp",
		Logo:  "static/icons/logo.webp",
	}
}

func (u *ui) render(w http.ResponseWriter, r *http.Request, p components.PageProps, fragments ...string) {
	p.JS = u.staticHashName(r.Context(), p.JS)
	p.CSS = u.staticHashName(r.Context(), p.CSS)
	p.Icon = u.staticHashName(r.Context(), p.Icon)
	p.Logo = u.staticHashName(r.Context(), p.Logo)
	if target := r.Header.Get("HX-Target"); target != "" {
		// A handler declares the fragments it can produce. Do not let an arbitrary
		// client header select unrelated template fragments or an empty response.
		if !slices.Contains(fragments, target) {
			web.RespondError(w, r, fmt.Errorf("%w: invalid fragment target %q", web.ErrBadRequest, target))
			return
		}
		templ.Handler(components.Page(p), templ.WithFragments(target)).ServeHTTP(w, r)
		return
	}
	templ.Handler(components.Page(p)).ServeHTTP(w, r)
}

// editorResource captures the small differences between persisted text
// editors so loading, dirty-state handling, and partial saves share one path.
type editorResource struct {
	Name           string
	Fragment       string
	MissingIsEmpty bool
	Load           func(context.Context) (string, error)
	Save           func(context.Context, string) error
	Props          func(*components.ConfigurationProps) *components.EditorProps
	NewProps       func(string, string, string) components.EditorProps
}

func (u *ui) editorResources() []editorResource {
	return []editorResource{
		configResource(u.api),
		errorTemplateResource(u.api),
	}
}

func (r editorResource) load(ctx context.Context) (string, error) {
	value, err := r.Load(ctx)
	if r.MissingIsEmpty && errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return value, err
}

func configResource(a *api) editorResource {
	return editorResource{
		Name:           "config",
		Fragment:       components.FragmentConfigPanel,
		MissingIsEmpty: true,
		Load:           a.store.LoadConfig,
		Save:           a.saveConfig,
		Props: func(p *components.ConfigurationProps) *components.EditorProps {
			return &p.Config
		},
		NewProps: editorConfig,
	}
}

func errorTemplateResource(a *api) editorResource {
	return editorResource{
		Name:     "error_template",
		Fragment: components.FragmentErrorPanel,
		Load:     a.store.LoadErrorTemplate,
		Save:     a.saveErrorTemplate,
		Props: func(p *components.ConfigurationProps) *components.EditorProps {
			return &p.ErrorTemplate
		},
		NewProps: editorErrorTemplate,
	}
}

func errorFromProps(p components.EditorProps) error {
	if p.Error == "" {
		return nil
	}
	return errors.New(p.Error)
}

func editorConfig(value, baseline, err string) components.EditorProps {
	return components.EditorProps{
		ID:          "config-editor",
		Name:        "config",
		Title:       "Config",
		Description: "Starlark feed definitions and filters.",
		Placeholder: `feed(url = "https://example.com/rss.xml")`,
		Language:    "starlark",
		Value:       value,
		Baseline:    baseline,
		Error:       err,
		SaveURL:     "/config/config",
	}
}

func editorErrorTemplate(value, baseline, err string) components.EditorProps {
	return components.EditorProps{
		ID:          "error-template-editor",
		Name:        "error_template",
		Title:       "Error Template",
		Description: "Template used for posting error notifications.",
		Placeholder: "Fetch failed: %v",
		Language:    "template",
		Value:       value,
		Baseline:    baseline,
		Error:       err,
		SaveURL:     "/config/error-template",
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func formValue(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	if err := r.ParseForm(); err != nil {
		web.RespondError(w, r, fmt.Errorf("%w: invalid form payload", web.ErrBadRequest))
		return "", false
	}
	values, ok := r.Form[key]
	if !ok || len(values) == 0 {
		web.RespondError(w, r, fmt.Errorf("%w: missing %q form value", web.ErrBadRequest, key))
		return "", false
	}
	return values[0], true
}
