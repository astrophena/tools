// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"
	"io"
	"slices"
	"sync"
	"unicode/utf8"

	"go.astrophena.name/base/cli/progressbar"
)

type runProgress struct {
	bar *progressbar.Bar

	mu      sync.Mutex
	running []*Action // In start order.
}

func startProgress(w io.Writer, tasks int) *runProgress {
	p := &runProgress{bar: progressbar.New(w, tasks, true)}
	p.bar.Start()
	return p
}

func (p *runProgress) startAction(action *Action) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = append(p.running, action)
	p.updateTitle()
}

func (p *runProgress) finishAction(action *Action) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	i := slices.Index(p.running, action)
	p.running = slices.Delete(p.running, i, i+1)
	p.updateTitle()
}

func (p *runProgress) updateTitle() {
	n := len(p.running)
	if n == 0 {
		p.bar.SetTitle("")
		return
	}

	action := p.running[0]
	title := action.Title
	if title == "" {
		title = action.Summary
	}
	if n > 1 {
		title = fmt.Sprintf("(+%d) %s", n-1, title)
	}
	const maxTitleRunes = 28
	if utf8.RuneCountInString(title) > maxTitleRunes {
		title = fmt.Sprintf("%.*s…", maxTitleRunes-1, title)
	}
	p.bar.SetTitle(title)
}

func (p *runProgress) taskDone() {
	if p != nil {
		p.bar.Increment()
	}
}
