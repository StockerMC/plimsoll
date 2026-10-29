package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/StockerMC/plimsoll/internal/kube"
	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/recommender/ensemble"
	"github.com/StockerMC/plimsoll/internal/recommender/percentile"
)

type loop struct {
	client       dynamic.Interface
	name         string
	ensembleName string
	namespace    string
	window       time.Duration
	opts         percentile.Options
	ensembleOpts ensemble.Options
	checkpoints  bool
	// Keyed by VPA UID, so a VPA deleted and recreated under the same name
	// starts over.
	vpas map[types.UID]*vpaState
}

// vpaState is the state kept for one VPA between passes.
type vpaState struct {
	// workload the models were built for.
	target     string
	containers map[string]*container
	oom        *kube.OOMDetector
}

// container is the state kept for one container of one VPA between passes.
type container struct {
	// Recommender name the VPA selected when the model was built.
	name    string
	windows *kube.Aggregator
	model   recommender.Recommender
	// Set once the checkpoint was loaded or confirmed absent.
	restored bool
	// Last checkpoint written, to skip writes when nothing changed.
	saved []byte
	// Set once a window, an OOM or a restored checkpoint backs the model;
	// before that its recommendation is only the floor.
	supported bool
}

func (l *loop) runOnce(ctx context.Context) error {
	vpas, err := kube.ListVPAs(ctx, l.client, l.namespace, l.names()...)
	if err != nil {
		return err
	}
	listed := make(map[types.UID]bool)
	var errs []error
	for i := range vpas {
		listed[vpas[i].GetUID()] = true
		errs = append(errs, l.update(ctx, &vpas[i]))
	}
	for uid := range l.vpas {
		if !listed[uid] {
			delete(l.vpas, uid)
		}
	}
	return errors.Join(errs...)
}

func (l *loop) update(ctx context.Context, vpa *unstructured.Unstructured) error {
	target, err := kube.LookupTarget(ctx, l.client, vpa)
	if err != nil {
		return err
	}
	st := l.state(vpa)
	for name := range st.containers {
		if !slices.Contains(target.Containers, name) {
			delete(st.containers, name)
		}
	}
	// A failed read is retried next pass before anything is observed, saved
	// or published, so a transient error cannot overwrite the checkpoint.
	for _, name := range target.Containers {
		if err := l.restore(ctx, vpa, name, l.container(st, vpa, name)); err != nil {
			return err
		}
	}
	// Pod status does not come from metrics-server, so OOMs are still seen
	// and recommendations still published while usage cannot be read.
	usage, usageErr := kube.ListMemoryUsage(ctx, l.client, vpa.GetNamespace(), target.Selector)
	pods, err := kube.ListPods(ctx, l.client, vpa.GetNamespace(), target.Selector)
	if err != nil {
		return errors.Join(usageErr, err)
	}
	policies := make(map[string]kube.MemoryPolicy)
	for _, name := range target.Containers {
		if policies[name], err = kube.ContainerMemoryPolicy(vpa, name); err != nil {
			return errors.Join(usageErr, err)
		}
	}
	ooms := st.oom.Scan(pods)
	var rec kube.Recommendation
	for _, name := range target.Containers {
		c := l.container(st, vpa, name)
		var readings []kube.MemoryUsage
		for _, u := range usage {
			if u.Container == name {
				readings = append(readings, u)
			}
		}
		for _, s := range c.windows.AddAll(readings) {
			c.model.Observe(s)
			c.supported = true
		}
		for _, o := range ooms {
			if o.Container == name {
				l.recordOOM(c, o)
			}
		}
		if l.checkpoints {
			l.save(ctx, vpa, name, c)
		}
		p := policies[name]
		if p.Skip {
			continue
		}
		if !c.supported {
			if old, ok := kube.CurrentRecommendation(vpa, name); ok {
				rec.ContainerRecommendations = append(rec.ContainerRecommendations, old)
			}
			continue
		}
		r := c.model.Recommend()
		r.Target, r.Lower, r.Upper = p.Cap(r.Target), p.Cap(r.Lower), p.Cap(r.Upper)
		rec.ContainerRecommendations = append(rec.ContainerRecommendations, kube.ContainerRecommendation{
			ContainerName: name,
			Target:        memory(r.Target),
			LowerBound:    memory(r.Lower),
			UpperBound:    memory(r.Upper),
		})
		log.Printf("%s/%s %s: target %s lower %s upper %s", vpa.GetNamespace(), vpa.GetName(), name,
			quantity(r.Target), quantity(r.Lower), quantity(r.Upper))
	}
	if !slices.ContainsFunc(target.Containers, func(name string) bool { return st.containers[name].supported }) {
		log.Printf("%s/%s: no completed window yet, status left as it is", vpa.GetNamespace(), vpa.GetName())
		return usageErr
	}
	return errors.Join(usageErr, kube.PatchStatus(ctx, l.client, vpa, kube.ProvidedStatus(vpa, rec, time.Now())))
}

func (l *loop) names() []string {
	if l.ensembleName == "" {
		return []string{l.name}
	}
	return []string{l.name, l.ensembleName}
}

// workload names what a VPA's models describe: the VPA object and its
// targetRef.
func workload(vpa *unstructured.Unstructured) string {
	kind, _, _ := unstructured.NestedString(vpa.Object, "spec", "targetRef", "kind")
	name, _, _ := unstructured.NestedString(vpa.Object, "spec", "targetRef", "name")
	return string(vpa.GetUID()) + "/" + kind + "/" + name
}

// state returns the state for one VPA, starting over when its targetRef has
// changed since the models were built.
func (l *loop) state(vpa *unstructured.Unstructured) *vpaState {
	target := workload(vpa)
	st, ok := l.vpas[vpa.GetUID()]
	if !ok || st.target != target {
		st = &vpaState{target: target, containers: make(map[string]*container), oom: kube.NewOOMDetector()}
		l.vpas[vpa.GetUID()] = st
	}
	return st
}

// container returns the state for one container, starting over when the
// VPA has switched between our recommender names since it was built.
func (l *loop) container(st *vpaState, vpa *unstructured.Unstructured, name string) *container {
	selected, _ := kube.SelectedRecommender(vpa, l.names()...)
	c, ok := st.containers[name]
	if !ok || c.name != selected {
		c = &container{name: selected, windows: kube.NewAggregator(l.window), model: l.model(selected)}
		st.containers[name] = c
	}
	return c
}

func (l *loop) model(selected string) recommender.Recommender {
	if selected == l.ensembleName {
		return ensemble.New(l.ensembleOpts)
	}
	return percentile.New(l.opts)
}

func (l *loop) restore(ctx context.Context, vpa *unstructured.Unstructured, name string, c *container) error {
	if c.restored {
		return nil
	}
	cp, ok := c.model.(recommender.Checkpointer)
	if !l.checkpoints || !ok {
		c.restored = true
		return nil
	}
	saved, found, err := kube.GetCheckpoint(ctx, l.client, vpa.GetNamespace(), vpa.GetName(), name, workload(vpa))
	if errors.Is(err, kube.ErrUnusableCheckpoint) {
		log.Printf("%v; starting cold", err)
		c.restored = true
		return nil
	}
	if err != nil {
		return err
	}
	c.restored = true
	if !found {
		return nil
	}
	if err := cp.Restore(saved); err != nil {
		log.Printf("checkpoint %s/%s %s: %v; starting cold", vpa.GetNamespace(), vpa.GetName(), name, err)
		return nil
	}
	c.supported = true
	log.Printf("checkpoint %s/%s %s: restored %d samples from %s", vpa.GetNamespace(), vpa.GetName(), name,
		saved.Samples, saved.First.Format(time.RFC3339))
	return nil
}

func (l *loop) save(ctx context.Context, vpa *unstructured.Unstructured, name string, c *container) {
	cp, ok := c.model.(recommender.Checkpointer)
	if !ok {
		return
	}
	state, err := cp.Checkpoint()
	if err != nil {
		log.Printf("checkpoint %s/%s %s: %v", vpa.GetNamespace(), vpa.GetName(), name, err)
		return
	}
	raw, err := json.Marshal(state)
	if err != nil || bytes.Equal(raw, c.saved) {
		return
	}
	if err := kube.PutCheckpoint(ctx, l.client, vpa.GetNamespace(), vpa.GetName(), name, workload(vpa), state, time.Now()); err != nil {
		log.Print(err)
		return
	}
	c.saved = raw
}

func (l *loop) recordOOM(c *container, o kube.OOM) {
	if err := c.model.RecordOOM(o.Pod, o.Time, o.Request); err != nil {
		log.Printf("oom %s/%s: %v", o.Pod, o.Container, err)
		return
	}
	c.supported = true
	log.Printf("oom %s/%s at %s: recorded request %s", o.Pod, o.Container, o.Time.Format(time.TimeOnly), quantity(o.Request))
}

func memory(bytes float64) corev1.ResourceList {
	return corev1.ResourceList{corev1.ResourceMemory: *quantity(bytes)}
}

func quantity(bytes float64) *resource.Quantity {
	return resource.NewQuantity(int64(math.Ceil(bytes)), resource.BinarySI)
}
