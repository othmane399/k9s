package view

import (
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/render"
	"github.com/derailed/k9s/internal/ui"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

type DaemonSet struct {
	ResourceViewer
}

func NewDaemonSet(gvr dao.GVR) ResourceViewer {
	d := DaemonSet{
		ResourceViewer: NewRestartExtender(
			NewLogsExtender(
				NewGeneric(gvr),
				func() string { return "" },
			),
		),
	}
	d.BindKeys()
	d.GetTable().SetEnterFn(d.showPods)
	d.GetTable().SetColorerFn(render.DaemonSet{}.ColorerFunc())

	return &d
}

func (d *DaemonSet) BindKeys() {
	d.Actions().Add(ui.KeyActions{
		ui.KeyShiftD: ui.NewKeyAction("Sort Desired", d.GetTable().SortColCmd(1, true), false),
		ui.KeyShiftC: ui.NewKeyAction("Sort Current", d.GetTable().SortColCmd(2, true), false),
	})
}

func (d *DaemonSet) showPods(app *App, _, res, sel string) {
	ns, n := namespaced(sel)
	o, err := app.factory.Get(ns, d.GVR(), n, labels.Everything())
	if err != nil {
		d.App().Flash().Err(err)
		return
	}

	var ds appsv1.DaemonSet
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.(*unstructured.Unstructured).Object, &ds)
	if err != nil {
		d.App().Flash().Err(err)
	}

	showPodsFromSelector(app, ns, ds.Spec.Selector)
}
