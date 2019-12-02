package render

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
)

// Job renders a K8s Job to screen.
type Job struct{}

// ColorerFunc colors a resource row.
func (Job) ColorerFunc() ColorerFunc {
	return DefaultColorer
}

// Header returns a header row.
func (Job) Header(ns string) HeaderRow {
	var h HeaderRow
	if isAllNamespace(ns) {
		h = append(h, Header{Name: "NAMESPACE"})
	}

	return append(h,
		Header{Name: "NAME"},
		Header{Name: "COMPLETIONS"},
		Header{Name: "DURATION"},
		Header{Name: "CONTAINERS"},
		Header{Name: "IMAGES"},
		Header{Name: "AGE", Decorator: ageDecorator},
	)
}

// Render renders a K8s resource to screen.
func (Job) Render(o interface{}, ns string, r *Row) error {
	raw, ok := o.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("Expected Job, but got %T", o)
	}
	var j batchv1.Job
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw.Object, &j)
	if err != nil {
		return err
	}

	cc, ii := toContainers(j.Spec.Template.Spec)

	fields := make(Fields, 0, len(r.Fields))
	if isAllNamespace(ns) {
		fields = append(fields, j.Namespace)
	}
	fields = append(fields,
		j.Name,
		toCompletion(j.Spec, j.Status),
		toDuration(j.Status),
		cc,
		ii,
		toAge(j.ObjectMeta.CreationTimestamp),
	)

	r.ID, r.Fields = MetaFQN(j.ObjectMeta), fields

	return nil
}

// Helpers...

const maxShow = 2

func toContainers(p v1.PodSpec) (string, string) {
	cc, ii := parseContainers(p.InitContainers)
	cn, ci := parseContainers(p.Containers)

	cc, ii = append(cc, cn...), append(ii, ci...)

	// Limit to 2 of each...
	if len(cc) > maxShow {
		cc = append(cc[:2], "(+"+strconv.Itoa(len(cc)-maxShow)+")...")
	}
	if len(ii) > maxShow {
		ii = append(ii[:2], "(+"+strconv.Itoa(len(ii)-maxShow)+")...")
	}

	return strings.Join(cc, ","), strings.Join(ii, ",")
}

func parseContainers(cos []v1.Container) (nn, ii []string) {
	for _, co := range cos {
		nn = append(nn, co.Name)
		ii = append(ii, co.Image)
	}

	return nn, ii
}

func toCompletion(spec batchv1.JobSpec, status batchv1.JobStatus) (s string) {
	if spec.Completions != nil {
		return strconv.Itoa(int(status.Succeeded)) + "/" + strconv.Itoa(int(*spec.Completions))
	}

	if spec.Parallelism == nil {
		return strconv.Itoa(int(status.Succeeded)) + "/1"
	}

	p := *spec.Parallelism
	if p > 1 {
		return strconv.Itoa(int(status.Succeeded)) + "/1 of " + strconv.Itoa(int(p))
	}

	return strconv.Itoa(int(status.Succeeded)) + "/1"
}

func toDuration(status batchv1.JobStatus) string {
	if status.StartTime == nil {
		return MissingValue
	}

	var d time.Duration
	switch {
	case status.CompletionTime == nil:
		d = time.Since(status.StartTime.Time)
	default:
		d = status.CompletionTime.Sub(status.StartTime.Time)
	}

	return duration.HumanDuration(d)
}
