package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/kube"
	"github.com/StockerMC/plimsoll/internal/recommender/ensemble"
	"github.com/StockerMC/plimsoll/internal/recommender/ml"
	"github.com/StockerMC/plimsoll/internal/recommender/percentile"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig; in-cluster config when empty")
	name := flag.String("recommender-name", "plimsoll", "handle VPAs whose spec.recommenders names this")
	ensembleName := flag.String("ensemble-recommender-name", "plimsoll-ensemble", "serve the ensemble to VPAs naming this; empty disables it")
	namespace := flag.String("vpa-object-namespace", "", "only handle VPAs in this namespace; all namespaces when empty")
	interval := flag.Duration("recommender-interval", time.Minute, "time between passes")
	window := flag.Duration("memory-window", 5*time.Minute, "window over which each pod's samples are reduced to one peak")
	aggregation := flag.Duration("memory-aggregation-interval", 24*time.Hour, "window over which each pod keeps one peak in the histogram")
	halfLife := flag.Duration("memory-histogram-decay-half-life", 24*time.Hour, "half-life of the histogram's decay")
	minMemoryMB := flag.Float64("pod-recommendation-min-memory-mb", 250, "floor on every memory recommendation")
	fallFraction := flag.Float64("ensemble-fall-fraction", 0.1, "fraction the ensemble's hourly max must fall by before the recommendation follows it")
	fallWindows := flag.Int("ensemble-fall-windows", 0, "memory windows in a row the fall must last; 0 follows every fall at once")
	overrunWeight := flag.Float64("ensemble-overrun-weight", ensemble.DefaultWeights.Overrun, "selector cost weight on usage above the limit")
	limitChangeWeight := flag.Float64("ensemble-limit-change-weight", ensemble.DefaultWeights.LimitChange, "selector cost weight on a change of limit")
	ratioWindows := flag.Int("ensemble-ratio-windows", 0, "memory windows over which each arg-min arm learns its margin from its peak-to-limit ratios; 0 keeps the fixed margins")
	ratioFloor := flag.Float64("ensemble-ratio-floor", 1, "floor on the learned margin, as a factor on the raw limit")
	timeScale := flag.Float64("ensemble-time-scale", 1, "divide the ensemble's half-lives by this, for workloads that replay a trace faster than real time")
	hourWindows := flag.Int("ensemble-hour-windows", 12, "memory windows in the ensemble's max and cold start, the paper's hour")
	checkpoints := flag.Bool("checkpoints", true, "restore from and save to VerticalPodAutoscalerCheckpoint objects")
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
		client:       client,
		name:         *name,
		ensembleName: *ensembleName,
		namespace:    *namespace,
		window:       *window,
		opts: percentile.Options{
			Buckets:             buckets,
			SamplesPerDay:       float64(24 * time.Hour / *window),
			MinMemory:           *minMemoryMB * 1024 * 1024,
			AggregationInterval: *aggregation,
			HalfLife:            *halfLife,
		},
		ensembleOpts: ensemble.Options{
			Buckets:    buckets,
			MinMemory:  *minMemoryMB * 1024 * 1024,
			LargeLimit: 1 << 30,
			OOMMinBump: 100 << 20,
			Weights: ml.Weights{
				Overrun:     *overrunWeight,
				Underrun:    ensemble.DefaultWeights.Underrun,
				LimitChange: *limitChangeWeight,
				ModelChange: ensemble.DefaultWeights.ModelChange,
			},
			FallFraction: *fallFraction,
			FallWindows:  *fallWindows,
			RatioWindows: *ratioWindows,
			RatioFloor:   *ratioFloor,
			TimeScale:    *timeScale,
			HourWindows:  *hourWindows,
			Window:       *window,
		},
		checkpoints: *checkpoints,
		vpas:        make(map[types.UID]*vpaState),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for {
		err := l.runOnce(ctx)
		if err != nil {
			log.Print(err)
		}
		if *once {
			if err != nil {
				stop()
				os.Exit(1)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(*interval):
		}
	}
}
