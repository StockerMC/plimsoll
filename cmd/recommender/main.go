package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/dynamic"

	"github.com/StockerMC/plimsoll/internal/kube"
)

var placeholderTarget = resource.MustParse("256Mi")

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig; in-cluster config when empty")
	name := flag.String("recommender-name", "plimsoll", "handle VPAs whose spec.recommenders names this")
	interval := flag.Duration("interval", time.Minute, "time between passes")
	once := flag.Bool("once", false, "run one pass and exit")
	flag.Parse()

	client, err := kube.NewDynamicClient(*kubeconfig)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for {
		if err := recommend(ctx, client, *name); err != nil {
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

func recommend(ctx context.Context, client dynamic.Interface, recommender string) error {
	vpas, err := kube.ListVPAs(ctx, client, recommender)
	if err != nil {
		return err
	}
	for i := range vpas {
		vpa := &vpas[i]
		containers, err := kube.TargetContainers(ctx, client, vpa)
		if err != nil {
			log.Print(err)
			continue
		}
		var rec kube.Recommendation
		for _, c := range containers {
			rec.ContainerRecommendations = append(rec.ContainerRecommendations, kube.ContainerRecommendation{
				ContainerName: c,
				Target:        corev1.ResourceList{corev1.ResourceMemory: placeholderTarget},
			})
		}
		if err := kube.PatchStatus(ctx, client, vpa, kube.ProvidedStatus(vpa, rec, time.Now())); err != nil {
			log.Print(err)
			continue
		}
		log.Printf("%s/%s: memory target %s for %d containers", vpa.GetNamespace(), vpa.GetName(), placeholderTarget.String(), len(containers))
	}
	return nil
}
