package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	containers   map[string]*container
	oom          map[string]*kube.OOMDetector
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
}

func (l *loop) runOnce(ctx context.Context) error {
	vpas, err := kube.ListVPAs(ctx, l.client, l.namespace, l.names()...)
	if err != nil {
		return err
	}
	for i := range vpas {
		if err := l.update(ctx, &vpas[i]); err != nil {
			log.Print(err)
		}
	}
	return nil
}

func (l *loop) update(ctx context.Context, vpa *unstructured.Unstructured) error {
	target, err := kube.LookupTarget(ctx, l.client, vpa)
	if err != nil {
		return err
	}
	// A failed read is retried next pass before anything is observed, saved
	// or published, so a transient error cannot overwrite the checkpoint.
	for _, name := range target.Containers {
		if err := l.restore(ctx, vpa, name, l.container(vpa, name)); err != nil {
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
	ooms := l.detector(vpa).Scan(pods)
	var rec kube.Recommendation
	for _, name := range target.Containers {
		c := l.container(vpa, name)
		var readings []kube.MemoryUsage
		for _, u := range usage {
			if u.Container == name {
				readings = append(readings, u)
			}
		}
		for _, s := range c.windows.AddAll(readings) {
			c.model.Observe(s)
		}
		for _, o := range ooms {
			if o.Container == name {
				l.recordOOM(c, o)
			}
		}
		r := c.model.Recommend()
		rec.ContainerRecommendations = append(rec.ContainerRecommendations, kube.ContainerRecommendation{
			ContainerName: name,
			Target:        memory(r.Target),
			LowerBound:    memory(r.Lower),
			UpperBound:    memory(r.Upper),
		})
		log.Printf("%s/%s %s: target %s lower %s upper %s", vpa.GetNamespace(), vpa.GetName(), name,
			quantity(r.Target), quantity(r.Lower), quantity(r.Upper))
		if l.checkpoints {
			l.save(ctx, vpa, name, c)
		}
	}
	return errors.Join(usageErr, kube.PatchStatus(ctx, l.client, vpa, kube.ProvidedStatus(vpa, rec, time.Now())))
}

func (l *loop) names() []string {
	if l.ensembleName == "" {
		return []string{l.name}
	}
	return []string{l.name, l.ensembleName}
}

// container returns the state for one container, starting over when the
// VPA has switched between our recommender names since it was built.
func (l *loop) container(vpa *unstructured.Unstructured, name string) *container {
	key := fmt.Sprintf("%s/%s/%s", vpa.GetNamespace(), vpa.GetName(), name)
	selected, _ := kube.SelectedRecommender(vpa, l.names()...)
	c, ok := l.containers[key]
	if !ok || c.name != selected {
		c = &container{name: selected, windows: kube.NewAggregator(l.window), model: l.model(selected)}
		l.containers[key] = c
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
	saved, found, err := kube.GetCheckpoint(ctx, l.client, vpa.GetNamespace(), vpa.GetName(), name)
	if errors.Is(err, kube.ErrUnusableCheckpoint) {
		log.Print(err)
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
		log.Printf("checkpoint %s/%s %s: %v", vpa.GetNamespace(), vpa.GetName(), name, err)
		return nil
	}
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
	if err := kube.PutCheckpoint(ctx, l.client, vpa.GetNamespace(), vpa.GetName(), name, state, time.Now()); err != nil {
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
	log.Printf("oom %s/%s at %s: recorded request %s", o.Pod, o.Container, o.Time.Format(time.TimeOnly), quantity(o.Request))
}

func (l *loop) detector(vpa *unstructured.Unstructured) *kube.OOMDetector {
	key := vpa.GetNamespace() + "/" + vpa.GetName()
	d, ok := l.oom[key]
	if !ok {
		d = kube.NewOOMDetector()
		l.oom[key] = d
	}
	return d
}

func memory(bytes float64) corev1.ResourceList {
	return corev1.ResourceList{corev1.ResourceMemory: *quantity(bytes)}
}

func quantity(bytes float64) *resource.Quantity {
	return resource.NewQuantity(int64(math.Ceil(bytes)), resource.BinarySI)
}
