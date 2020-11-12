package main

import (
	"context"
	"os"
	"runtime"

	"github.com/containerssh/log"
	"github.com/containerssh/log/pipeline"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/janoszen/exoscale-account-wiper/plugin"
)

func main() {
	logger := pipeline.NewLoggerPipelineFactory(&logFormatter{}, os.Stdout).Make(log.LevelNotice)

	if len(os.Args) > 1 {
		if len(os.Args) > 2 {
			logger.Errorf("invalid number of arguments: %d", len(os.Args))
		}
		switch os.Args[1] {
		case "-h":
			println("Usage: hand-in-automation [-v|-vv]")
			os.Exit(0)
		case "-v":
			logger.SetLevel(log.LevelInfo)
		case "-vv":
			logger.SetLevel(log.LevelDebug)
		}
	}

	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		logger.Errorf("failed to get cwd (%v)", err)
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
		logger.Errorf("EXOSCALE_KEY and EXOSCALE_SECRET must be provided")
		defer os.Exit(1)
		runtime.Goexit()
	}

	clientFactory := plugin.NewClientFactory(apiKey, apiSecret)

	logger.Infof("running preflight checks...")

	logger.Debugf("checking Docker socket...")
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Infof("failed to create Docker client (%v)", err)
		defer os.Exit(1)
		runtime.Goexit()
	}
	dockerClient.NegotiateAPIVersion(ctx)

	if _, err := dockerClient.Ping(ctx); err != nil {
		logger.Errorf("error: could not ping Docker socket (%v)\n", err)
		os.Exit(1)
	}
	logger.Debugf("check!")

	logger.Debugf("pulling Terraform execution image...")
	pullResult, err := dockerClient.ImagePull(ctx, "docker.io/janoszen/terraform", types.ImagePullOptions{})
	if err != nil {
		logger.Errorf("error: could not pull Terraform image (%v)\n", err)
		os.Exit(1)
	}
	if _, err := dockerToLogger(pullResult, logger); err != nil {
		logger.Errorf("error: could not pull Terraform image (%v)\n", err)
		os.Exit(1)
	}
	logger.Debugf("check!")

	logger.Debugf("checking Exoscale credentials...")
	if _, err := clientFactory.GetExoscaleClient().RequestWithContext(ctx, egoscale.ListZones{}); err != nil {
		logger.Errorf("error: could not list Exoscale zones (%v)\n", err)
		os.Exit(1)
	}
	logger.Debugf("check!")
	logger.Debugf("preflight checks complete, ready for takeoff.")

	logger.Infof("checking code at %s, this will take a long time...", directory)
	err = runTests(ctx, clientFactory, dockerClient, directory, logger)
	if err != nil {
		logger.Errorf("run failed: %v", err)
	} else {
		logger.Noticef("run successful")
	}
}
