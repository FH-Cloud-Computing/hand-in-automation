package main

import (
	"context"
	"log"
	"os"
	"runtime"

	"github.com/docker/docker/client"
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

	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Printf("failed to create Docker client (%v)", err)
		defer os.Exit(1)
		runtime.Goexit()
	}
	dockerClient.NegotiateAPIVersion(ctx)

	logFile := os.Stdout
	log.Printf("Checking code at %s...", directory)
	err = runTests(ctx, clientFactory, dockerClient, directory, logFile)
	if err != nil {
		log.Printf("Run failed: %v", err)
	} else {
		log.Printf("Run successful")
	}
}
