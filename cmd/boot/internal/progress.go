// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"
	"io"
	"slices"
	"sync"

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
	switch len(p.running) {
	case 0:
		p.bar.SetTitle("")
	case 1:
		p.bar.SetTitle(p.running[0].Summary)
	default:
		p.bar.SetTitle(fmt.Sprintf("(+%d) %s", len(p.running)-1, p.running[0].Summary))
	}
}

func (p *runProgress) taskDone() {
	if p != nil {
		p.bar.Increment()
	}
}
