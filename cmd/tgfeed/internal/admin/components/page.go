// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package components

import (
	"time"

	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
)

const (
	// FragmentAppShell identifies the stable dashboard shell.
	FragmentAppShell = "app-shell"
	// FragmentDashboardContent identifies the content swapped during navigation.
	FragmentDashboardContent = "dashboard-content"
	// FragmentStatsContent identifies the independently refreshed stats view.
	FragmentStatsContent = "stats-content"
	// FragmentConfigPanel identifies the configuration editor response.
	FragmentConfigPanel = "config-panel"
	// FragmentErrorPanel identifies the error-template editor response.
	FragmentErrorPanel = "error-template-panel"
)

// Route identifies a dashboard section.
type Route string

const (
	// RouteStats identifies the run statistics dashboard.
	RouteStats Route = "stats"
	// RouteConfiguration identifies the configuration editor.
	RouteConfiguration Route = "configuration"
)

// PageProps contains the shared application page state.
type PageProps struct {
	// Title is the current section name used in browser metadata.
	Title string
	// Route controls navigation state and the dashboard body.
	Route Route
	// Banner contains optional feedback for a page-level action.
	Banner string
	// JS is the cache-busted application module path.
	JS string
	// CSS is the cache-busted application stylesheet path.
	CSS string
	// Icon is the cache-busted favicon path.
	Icon string
	// Logo is the cache-busted header logo path.
	Logo string
	// Stats contains the stats section model when Route is [RouteStats].
	Stats *StatsProps
	// Configuration contains the editor model when Route is [RouteConfiguration].
	Configuration *ConfigurationProps
}

// StatsProps contains data rendered by the statistics dashboard.
type StatsProps struct {
	// Runs contains newest-first summaries used by trends and comparisons.
	Runs []stats.RunSummary
	// Active contains full details for the latest or selected run.
	Active *stats.Run
	// SelectedAt pins Active to a run; zero tracks the latest run.
	SelectedAt int64
	// AutoRefresh enables periodic replacement of the stats fragment.
	AutoRefresh bool
	// DetailsOpen preserves the expanded analytics panel across requests.
	DetailsOpen bool
	// RefreshedAt anchors timestamps and records when the model was loaded.
	RefreshedAt time.Time
	// Error contains a user-visible stats loading error.
	Error string
}

// ConfigurationProps contains the two editable resources.
type ConfigurationProps struct {
	// Config describes the Starlark configuration editor.
	Config EditorProps
	// ErrorTemplate describes the error notification template editor.
	ErrorTemplate EditorProps
}

// EditorProps describes one CodeMirror-enhanced text resource.
type EditorProps struct {
	// ID identifies the textarea enhanced by CodeMirror.
	ID string
	// Name is the form field used by the save handler.
	Name string
	// Title labels the editor panel.
	Title string
	// Description explains the persisted resource represented by the editor.
	Description string
	// Placeholder is shown when the resource is empty.
	Placeholder string
	// Language labels the editor and selects its presentation.
	Language string
	// Value is the text currently shown to the user.
	Value string
	// Baseline is the persisted text used to detect unsaved changes.
	Baseline string
	// Error contains feedback from loading, validation, or persistence.
	Error string
	// SaveURL receives an individual editor submission.
	SaveURL string
}

func pageTitle(title string) string {
	if title == "" {
		return "tgfeed"
	}
	return title + " · tgfeed"
}

func routeURL(route Route) string {
	if route == RouteConfiguration {
		return "/config"
	}
	return "/stats"
}

func editorStatus(p EditorProps) string {
	if p.Value != p.Baseline {
		return "Unsaved"
	}
	return "Synced"
}
