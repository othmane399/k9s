package view

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/perf"
	"github.com/derailed/k9s/internal/render"
	"github.com/derailed/k9s/internal/resource"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tview"
	"github.com/fsnotify/fsnotify"
	"github.com/gdamore/tcell"
	"github.com/rs/zerolog/log"
)

const (
	portForwardTitle = "PortForwards"
	promptPage       = "prompt"
)

// PortForward presents active portforward viewer.
type PortForward struct {
	*Table

	bench *perf.Benchmark
}

// NewPortForward returns a new viewer.
func NewPortForward(title, gvr string, list resource.List) ResourceViewer {
	return &PortForward{
		Table: NewTable(portForwardTitle),
	}
}

func (*PortForward) SetContextFn(ContextFunc) {}

// Init the view.
func (p *PortForward) Init(ctx context.Context) error {
	if err := p.Table.Init(ctx); err != nil {
		return err
	}
	p.registerActions()
	p.SetBorderFocusColor(tcell.ColorDodgerBlue)
	p.SetSelectedStyle(tcell.ColorWhite, tcell.ColorDodgerBlue, tcell.AttrNone)
	p.SetColorerFn(render.Forward{}.ColorerFunc())
	p.SetSortCol(p.NameColIndex()+6, 0, true)
	p.Select(1, 0)
	p.refresh()

	return nil
}

// GVR returns a resource descriptor.
func (p *PortForward) GVR() string {
	return "n/a"
}

// List returns the resource list.
func (p *PortForward) List() resource.List { return nil }

// GetTable returns the table view.
func (p *PortForward) GetTable() *Table { return p.Table }

// SetEnvFn sets the k9s env vars.
func (p *PortForward) SetEnvFn(EnvFunc) {}

// SetPath sets parent selector.
func (p *PortForward) SetPath(s string) {}

// Start runs the refresh loop.
func (p *PortForward) Start() {
	path := ui.BenchConfig(p.app.Config.K9s.CurrentCluster)
	var ctx context.Context
	ctx, p.cancelFn = context.WithCancel(context.Background())
	if err := watchFS(ctx, p.app, config.K9sHome, path, p.reload); err != nil {
		p.app.Flash().Errf("RuRoh! Unable to watch benchmarks directory %s : %s", config.K9sHome, err)
	}
}

// Name returns the component name.
func (p *PortForward) Name() string {
	return portForwardTitle
}

func (p *PortForward) reload() {
	path := ui.BenchConfig(p.app.Config.K9s.CurrentCluster)
	log.Debug().Msgf("Reloading Config %s", path)
	if err := p.app.Bench.Reload(path); err != nil {
		p.app.Flash().Err(err)
	}
	p.refresh()
}

func (p *PortForward) refresh() {
	p.Update(p.hydrate())
	p.app.SetFocus(p)
	p.UpdateTitle()
}

func (p *PortForward) registerActions() {
	p.Actions().Add(ui.KeyActions{
		tcell.KeyEnter: ui.NewKeyAction("Benchmarks", p.showBenchCmd, true),
		tcell.KeyCtrlB: ui.NewKeyAction("Bench", p.benchCmd, true),
		tcell.KeyCtrlK: ui.NewKeyAction("Bench Stop", p.benchStopCmd, true),
		tcell.KeyCtrlD: ui.NewKeyAction("Delete", p.deleteCmd, true),
		ui.KeySlash:    ui.NewKeyAction("Filter", p.activateCmd, false),
		tcell.KeyEsc:   ui.NewKeyAction("Back", p.app.PrevCmd, false),
		ui.KeyShiftP:   ui.NewKeyAction("Sort Ports", p.SortColCmd(2, true), false),
		ui.KeyShiftU:   ui.NewKeyAction("Sort URL", p.SortColCmd(4, true), false),
	})
}

func (p *PortForward) showBenchCmd(evt *tcell.EventKey) *tcell.EventKey {
	p.app.inject(NewBench("", "", nil))

	return nil
}

func (p *PortForward) benchStopCmd(evt *tcell.EventKey) *tcell.EventKey {
	if p.bench != nil {
		log.Debug().Msg(">>> Benchmark cancelFned!!")
		p.app.status(ui.FlashErr, "Benchmark Camceled!")
		p.bench.Cancel()
	}
	p.app.StatusReset()

	return nil
}

func (p *PortForward) benchCmd(evt *tcell.EventKey) *tcell.EventKey {
	sel := p.getSelectedItem()
	if sel == "" {
		return nil
	}

	if p.bench != nil {
		p.app.Flash().Err(errors.New("Only one benchmark allowed at a time"))
		return nil
	}

	r, _ := p.GetSelection()
	cfg, co := defaultConfig(), ui.TrimCell(p.SelectTable, r, 2)
	if b, ok := p.app.Bench.Benchmarks.Containers[containerID(sel, co)]; ok {
		cfg = b
	}
	cfg.Name = sel

	base := ui.TrimCell(p.SelectTable, r, 4)
	var err error
	if p.bench, err = perf.NewBenchmark(base, cfg); err != nil {
		p.app.Flash().Errf("Bench failed %v", err)
		p.app.StatusReset()
		return nil
	}

	p.app.status(ui.FlashWarn, "Benchmark in progress...")
	log.Debug().Msg("Bench starting...")
	go p.runBenchmark()

	return nil
}

func (p *PortForward) runBenchmark() {
	p.bench.Run(p.app.Config.K9s.CurrentCluster, func() {
		log.Debug().Msg("Bench Completed!")
		p.app.QueueUpdate(func() {
			if p.bench.Canceled() {
				p.app.status(ui.FlashInfo, "Benchmark cancelFned")
			} else {
				p.app.status(ui.FlashInfo, "Benchmark Completed!")
				p.bench.Cancel()
			}
			p.bench = nil
			go func() {
				<-time.After(2 * time.Second)
				p.app.QueueUpdate(func() { p.app.StatusReset() })
			}()
		})
	})
}

func (p *PortForward) getSelectedItem() string {
	r, _ := p.GetSelection()
	if r == 0 {
		return ""
	}
	return fwFQN(
		fqn(ui.TrimCell(p.SelectTable, r, 0), ui.TrimCell(p.SelectTable, r, 1)),
		ui.TrimCell(p.SelectTable, r, 2),
	)
}

func (p *PortForward) deleteCmd(evt *tcell.EventKey) *tcell.EventKey {
	if !p.SearchBuff().Empty() {
		p.SearchBuff().Reset()
		return nil
	}

	sel := p.getSelectedItem()
	if sel == "" {
		return nil
	}

	showModal(p.app.Content.Pages, fmt.Sprintf("Delete PortForward `%s?", sel), func() {
		stats := p.app.forwarders.Kill(sel)
		log.Debug().Msgf("Deleted %d port-forwards", stats)
		p.app.Flash().Infof("PortForward %s(%d) deleted!", sel, stats)
		p.Update(p.hydrate())
	})

	return nil
}

func (p *PortForward) hydrate() render.TableData {
	var re render.Forward

	data := render.TableData{
		Header:    re.Header(render.AllNamespaces),
		RowEvents: make(render.RowEvents, 0, len(p.app.forwarders)),
		Namespace: render.AllNamespaces,
	}

	containers := p.app.Bench.Benchmarks.Containers
	for _, f := range p.app.forwarders {
		fqn := containerID(f.Path(), f.Container())
		cfg := benchCfg{
			c: p.app.Bench.Benchmarks.Defaults.C,
			n: p.app.Bench.Benchmarks.Defaults.N,
		}
		if config, ok := containers[fqn]; ok {
			cfg.c, cfg.n = config.C, config.N
			cfg.host, cfg.path = config.HTTP.Host, config.HTTP.Path
		}

		var row render.Row
		fwd := forwarder{
			Forwarder:         f,
			BenchConfigurator: cfg,
		}
		if err := re.Render(fwd, render.AllNamespaces, &row); err != nil {
			log.Error().Err(err).Msgf("PortForward render failed")
			continue
		}
		data.RowEvents = append(data.RowEvents, render.RowEvent{Kind: render.EventAdd, Row: row})
	}

	return data
}

// ----------------------------------------------------------------------------
// Helpers...

var _ render.PortForwarder = forwarder{}

type forwarder struct {
	render.Forwarder
	render.BenchConfigurator
}

type benchCfg struct {
	c, n       int
	host, path string
}

func (b benchCfg) C() int           { return b.c }
func (b benchCfg) N() int           { return b.n }
func (b benchCfg) Host() string     { return b.host }
func (b benchCfg) HttpPath() string { return b.path }

func defaultConfig() config.BenchConfig {
	return config.BenchConfig{
		C: config.DefaultC,
		N: config.DefaultN,
		HTTP: config.HTTP{
			Method: config.DefaultMethod,
			Path:   "/",
		},
	}
}

func showModal(p *ui.Pages, msg string, ok func()) {
	m := tview.NewModal().
		AddButtons([]string{"Cancel", "OK"}).
		SetTextColor(tcell.ColorFuchsia).
		SetText(msg).
		SetDoneFunc(func(_ int, b string) {
			if b == "OK" {
				ok()
			}
			dismissModal(p)
		})
	m.SetTitle("<Delete Benchmark>")
	p.AddPage(promptPage, m, false, false)
	p.ShowPage(promptPage)
}

func dismissModal(p *ui.Pages) {
	p.RemovePage(promptPage)
}

func watchFS(ctx context.Context, app *App, dir, file string, cb func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case evt := <-w.Events:
				log.Debug().Msgf("FS %s event %v", file, evt.Name)
				if file == "" || evt.Name == file {
					log.Debug().Msgf("Capturing Event %#v", evt)
					app.QueueUpdateDraw(func() {
						cb()
					})
				}
			case err := <-w.Errors:
				log.Info().Err(err).Msgf("FS %s watcher failed", dir)
				return
			case <-ctx.Done():
				log.Debug().Msgf("<<FS %s WATCHER DONE>>", dir)
				if err := w.Close(); err != nil {
					log.Error().Err(err).Msg("Closing portforward watcher")
				}
				return
			}
		}
	}()

	return w.Add(dir)
}
