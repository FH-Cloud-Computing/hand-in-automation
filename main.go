package main

import (
	"context"
	"log"
	"os"
	"runtime"

	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/janoszen/exoscale-account-wiper/plugin"
)

func main() {
	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("failed to get cwd (%v)", err)
		defer os.Exit(1)
		runtime.Goexit()
	}

	apiKey := os.Getenv("EXOSCALE_KEY")
	apiSecret := os.Getenv("EXOSCALE_SECRET")
	directory := os.Getenv("DIRECTORY")
	if directory == "" {
		directory = cwd
	}

	if apiKey == "" || apiSecret == "" {
		log.Printf("EXOSCALE_KEY and EXOSCALE_SECRET must be provided")
		defer os.Exit(1)
		runtime.Goexit()
	}

	clientFactory := plugin.NewClientFactory(apiKey, apiSecret)

	log.Println("Running preflight checks...")

	log.Println("Docker...")
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Printf("failed to create Docker client (%v)", err)
		defer os.Exit(1)
		runtime.Goexit()
	}
	dockerClient.NegotiateAPIVersion(ctx)

	if _, err := dockerClient.Ping(ctx); err != nil {
		log.Printf("error: could not ping Docker socket (%v)\n", err)
		os.Exit(1)
	}
	log.Println("check!")

	log.Println("Exoscale...")
	if _, err := clientFactory.GetExoscaleClient().RequestWithContext(ctx, egoscale.ListZones{}); err != nil {
		log.Printf("error: could not list zones (%v)\n", err)
		os.Exit(1)
	}
	log.Println("check!")
	log.Println("Preflight checks complete, ready for takeoff.")

	logFile := os.Stdout
	log.Printf("Checking code at %s...", directory)
	err = runTests(ctx, clientFactory, dockerClient, directory, logFile)
	if err != nil {
		log.Printf("Run failed: %v", err)
	} else {
		log.Printf("Run successful")
	}
}
