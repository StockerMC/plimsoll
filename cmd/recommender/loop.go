package main

import (
	"context"
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
	"github.com/StockerMC/plimsoll/internal/recommender/percentile"
)

type loop struct {
	client     dynamic.Interface
	name       string
	namespace  string
	window     time.Duration
	opts       percentile.Options
	containers map[string]*container
	oom        map[string]*kube.OOMDetector
}

// container is the state kept for one container of one VPA between passes.
type container struct {
	windows *kube.Aggregator
	model   recommender.Recommender
}

func (l *loop) runOnce(ctx context.Context) error {
	vpas, err := kube.ListVPAs(ctx, l.client, l.namespace, l.name)
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
	usage, err := kube.ListMemoryUsage(ctx, l.client, vpa.GetNamespace(), target.Selector)
	if err != nil {
		return err
	}
	pods, err := kube.ListPods(ctx, l.client, vpa.GetNamespace(), target.Selector)
	if err != nil {
		return err
	}
	ooms := l.detector(vpa).Scan(pods)
	var rec kube.Recommendation
	for _, name := range target.Containers {
		c := l.container(vpa, name)
		for _, u := range usage {
			if u.Container != name {
				continue
			}
			if s, ok := c.windows.Add(u.Pod, u.Bytes, u.Time); ok {
				c.model.Observe(s)
			}
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
	}
	return kube.PatchStatus(ctx, l.client, vpa, kube.ProvidedStatus(vpa, rec, time.Now()))
}

func (l *loop) container(vpa *unstructured.Unstructured, name string) *container {
	key := fmt.Sprintf("%s/%s/%s", vpa.GetNamespace(), vpa.GetName(), name)
	c, ok := l.containers[key]
	if !ok {
		c = &container{windows: kube.NewAggregator(l.window), model: percentile.New(l.opts)}
		l.containers[key] = c
	}
	return c
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
