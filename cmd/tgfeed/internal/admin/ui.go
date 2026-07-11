// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/tgfeed/internal/admin/components"
	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"

	"github.com/a-h/templ"
)

type ui struct {
	api            *api
	staticHashName func(context.Context, string) string
}

func newUI(api *api, staticHashName func(context.Context, string) string) *ui {
	return &ui{api: api, staticHashName: staticHashName}
}

func (u *ui) handleStats(w http.ResponseWriter, r *http.Request) {
	p := u.statsPage(r)
	u.render(w, r, p)
}

func (u *ui) handleStatsNothingToSave(w http.ResponseWriter, r *http.Request) {
	p := u.statsPage(r)
	p.Banner = "Nothing to save"
	u.render(w, r, p)
}

func (u *ui) statsPage(r *http.Request) components.PageProps {
	p := u.page("Stats", "stats")
	auto, err := queryBool(r, "auto_refresh", false)
	if err != nil {
		auto = false
	}
	details, err := queryBool(r, "details", false)
	if err != nil {
		details = false
	}
	selected, err := queryInt64Optional(r, "started_at_unix")
	if err != nil {
		p.Stats = &components.StatsProps{Error: err.Error(), RefreshedAt: time.Now(), AutoRefresh: auto, DetailsOpen: details}
		return p
	}
	runs, err := u.api.statsStore.ListRunSummaries(r.Context(), 100, nil)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			p.Stats = &components.StatsProps{Error: fmt.Sprintf("failed to read stats: %v", err), RefreshedAt: time.Now(), AutoRefresh: auto, DetailsOpen: details}
		} else {
			p.Stats = &components.StatsProps{RefreshedAt: time.Now(), AutoRefresh: auto, DetailsOpen: details}
		}
		return p
	}
	var startedAt int64
	if selected != nil {
		startedAt = *selected
	} else if len(runs) > 0 {
		startedAt = runs[0].StartedAtUnix
	}
	var active *stats.Run
	if startedAt != 0 {
		raw, loadErr := u.api.statsStore.GetRunByStartedAt(r.Context(), startedAt)
		if loadErr == nil {
			active = new(stats.Run)
			if decodeErr := json.Unmarshal(raw, active); decodeErr != nil {
				err = decodeErr
			}
		} else if !errors.Is(loadErr, sql.ErrNoRows) && !errors.Is(loadErr, fs.ErrNotExist) {
			err = loadErr
		}
	}
	props := &components.StatsProps{Runs: runs, Active: active, AutoRefresh: auto, DetailsOpen: details, RefreshedAt: time.Now()}
	if selected != nil {
		props.SelectedAt = *selected
	}
	if err != nil {
		props.Error = fmt.Sprintf("failed to read stats: %v", err)
	}
	p.Stats = props
	return p
}

func (u *ui) handleConfiguration(w http.ResponseWriter, r *http.Request) {
	u.render(w, r, u.configurationPage(r, ""))
}

func (u *ui) configurationPage(r *http.Request, banner string) components.PageProps {
	p := u.page("Configuration", "configuration")
	p.Banner = banner
	config, configErr := u.api.store.LoadConfig(r.Context())
	errorTemplate, templateErr := u.api.store.LoadErrorTemplate(r.Context())
	p.Configuration = &components.ConfigurationProps{
		Config:        editorConfig(config, config, errorString(configErr)),
		ErrorTemplate: editorErrorTemplate(errorTemplate, errorTemplate, errorString(templateErr)),
	}
	return p
}

func (u *ui) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	value, ok := formValue(w, r, "config")
	if !ok {
		return
	}
	baseline, loadErr := u.api.store.LoadConfig(r.Context())
	if errors.Is(loadErr, fs.ErrNotExist) {
		loadErr = nil
		baseline = ""
	}
	err := loadErr
	if err == nil {
		err = u.saveConfig(r, value)
		if err == nil {
			baseline = value
		}
	}
	p := u.configurationPage(r, "")
	p.Configuration.Config = editorConfig(value, baseline, errorString(err))
	u.render(w, r, p)
}

func (u *ui) handleSaveErrorTemplate(w http.ResponseWriter, r *http.Request) {
	value, ok := formValue(w, r, "error_template")
	if !ok {
		return
	}
	baseline, loadErr := u.api.store.LoadErrorTemplate(r.Context())
	err := loadErr
	if err == nil {
		err = u.saveErrorTemplate(r, value)
		if err == nil {
			baseline = value
		}
	}
	p := u.configurationPage(r, "")
	p.Configuration.ErrorTemplate = editorErrorTemplate(value, baseline, errorString(err))
	u.render(w, r, p)
}

func (u *ui) handleSaveAll(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		web.RespondError(w, r, fmt.Errorf("%w: invalid form payload", web.ErrBadRequest))
		return
	}
	config, configOK := r.Form["config"]
	templateValue, templateOK := r.Form["error_template"]
	p := u.configurationPage(r, "Nothing to save")
	jobs, successes := 0, 0
	if configOK {
		value := config[0]
		baseline := p.Configuration.Config.Baseline
		if value != baseline {
			jobs++
			err := u.saveConfig(r, value)
			if err == nil {
				successes++
				baseline = value
			}
			p.Configuration.Config = editorConfig(value, baseline, errorString(err))
		}
	}
	if templateOK {
		value := templateValue[0]
		baseline := p.Configuration.ErrorTemplate.Baseline
		if value != baseline {
			jobs++
			err := u.saveErrorTemplate(r, value)
			if err == nil {
				successes++
				baseline = value
			}
			p.Configuration.ErrorTemplate = editorErrorTemplate(value, baseline, errorString(err))
		}
	}
	if jobs > 0 && successes == jobs {
		p.Banner = "All changes saved"
	} else if jobs > 0 {
		p.Banner = "Some changes failed to save"
	}
	u.render(w, r, p)
}

func (u *ui) saveConfig(r *http.Request, value string) error {
	if u.api.isRunLocked() {
		return fmt.Errorf("%w: cannot modify config: run is in progress", errConflict)
	}
	if err := u.api.validateConfigFn(r.Context(), value); err != nil {
		return fmt.Errorf("invalid config: %v", err)
	}
	if err := u.api.store.SaveConfig(r.Context(), value); err != nil {
		return fmt.Errorf("failed to write config: %v", err)
	}
	return nil
}

func (u *ui) saveErrorTemplate(r *http.Request, value string) error {
	if u.api.isRunLocked() {
		return fmt.Errorf("%w: cannot modify error template: run is in progress", errConflict)
	}
	if err := u.api.store.SaveErrorTemplate(r.Context(), value); err != nil {
		return fmt.Errorf("failed to write error template: %v", err)
	}
	return nil
}

func (u *ui) page(title, route string) components.PageProps {
	return components.PageProps{Title: title, Route: route, JS: "static/js/app.min.js", CSS: "static/css/app.min.css", Icon: "static/icons/icon.webp", Logo: "static/icons/logo.webp"}
}

func (u *ui) render(w http.ResponseWriter, r *http.Request, p components.PageProps) {
	p.JS = u.staticHashName(r.Context(), p.JS)
	p.CSS = u.staticHashName(r.Context(), p.CSS)
	p.Icon = u.staticHashName(r.Context(), p.Icon)
	p.Logo = u.staticHashName(r.Context(), p.Logo)
	var options []func(*templ.ComponentHandler)
	if target := r.Header.Get("HX-Target"); target != "" {
		options = append(options, templ.WithFragments(target))
	}
	templ.Handler(components.Page(p), options...).ServeHTTP(w, r)
}

func editorConfig(value, baseline, err string) components.EditorProps {
	return components.EditorProps{ID: "config-editor", Name: "config", Title: "Config", Description: "Starlark feed definitions and filters.", Placeholder: `feed(url = "https://example.com/rss.xml")`, Language: "starlark", Value: value, Baseline: baseline, Error: err, SaveURL: "/config/config"}
}
func editorErrorTemplate(value, baseline, err string) components.EditorProps {
	return components.EditorProps{ID: "error-template-editor", Name: "error_template", Title: "Error Template", Description: "Template used for posting error notifications.", Placeholder: "Fetch failed: %v", Language: "template", Value: value, Baseline: baseline, Error: err, SaveURL: "/config/error-template"}
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
