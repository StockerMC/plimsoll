package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/kube"
	"github.com/StockerMC/plimsoll/internal/recommender/percentile"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig; in-cluster config when empty")
	name := flag.String("recommender-name", "plimsoll", "handle VPAs whose spec.recommenders names this")
	namespace := flag.String("vpa-object-namespace", "", "only handle VPAs in this namespace; all namespaces when empty")
	interval := flag.Duration("recommender-interval", time.Minute, "time between passes")
	window := flag.Duration("memory-window", 5*time.Minute, "window over which each pod's samples are reduced to one peak")
	aggregation := flag.Duration("memory-aggregation-interval", 24*time.Hour, "window over which each pod keeps one peak in the histogram")
	halfLife := flag.Duration("memory-histogram-decay-half-life", 24*time.Hour, "half-life of the histogram's decay")
	minMemoryMB := flag.Float64("pod-recommendation-min-memory-mb", 250, "floor on every memory recommendation")
	once := flag.Bool("once", false, "run one pass and exit")
	flag.Parse()

	client, err := kube.NewDynamicClient(*kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	// Upstream's memory buckets from model/aggregations_config.go.
	buckets, err := histogram.NewExponentialHistogramOptions(1e12, 1e7, 1.05, 1e-4)
	if err != nil {
		log.Fatal(err)
	}
	l := &loop{
		client:    client,
		name:      *name,
		namespace: *namespace,
		window:    *window,
		opts: percentile.Options{
			Buckets:             buckets,
			SamplesPerDay:       float64(24 * time.Hour / *window),
			MinMemory:           *minMemoryMB * 1024 * 1024,
			AggregationInterval: *aggregation,
			HalfLife:            *halfLife,
		},
		containers: make(map[string]*container),
		oom:        make(map[string]*kube.OOMDetector),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for {
		if err := l.runOnce(ctx); err != nil {
			log.Print(err)
		}
		if *once {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(*interval):
		}
	}
}
