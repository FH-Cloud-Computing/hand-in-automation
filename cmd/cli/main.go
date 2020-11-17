package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/containerssh/log"
	"github.com/containerssh/log/pipeline"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/janoszen/exoscale-account-wiper/plugin"

	handin "github.com/FH-Cloud-Computing/hand-in-automation"
	log2 "github.com/FH-Cloud-Computing/hand-in-automation/logger"
)

func main() {
	logger := pipeline.NewLoggerPipelineFactory(&log2.LogFormatter{}, os.Stdout).Make(log.LevelNotice)

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
	batch := os.Getenv("BATCH")
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
		logger.Errorf("failed to create Docker client (%v)", err)
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
	if _, err := handin.DockerToLogger(pullResult, logger); err != nil {
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

	if batch == "" {
		_ = runSingleTest(logger, directory, ctx, clientFactory, dockerClient)
	} else {
		dirs, err := filepath.Glob(path.Join(directory, "*"))
		if err != nil {
			logger.Errorf("error while running batch (%v)", err)
		}
		for _, dir := range dirs {
			func() {
				info, err := os.Stat(dir)
				if err != nil {
					logger.Warningf("failed to stat %s (%v)", dir, err)
					return
				}
				if info.Name() == directory {
					return
				}
				if info.Name() == ".git" {
					return
				}
				if info.IsDir() && !strings.HasPrefix(info.Name(), ".") {
					p := path.Join(directory, info.Name())
					successFile := fmt.Sprintf("%s.success", p)
					if _, err := os.Stat(successFile); err == nil {
						logger.Noticef("skipping %s, already successful", info.Name())
						return
					}
					failedFile := fmt.Sprintf("%s.failed", p)
					if _, err := os.Stat(failedFile); err == nil {
						logger.Noticef("skipping %s, already failed", info.Name())
						return
					}

					logFile, err := os.Create(fmt.Sprintf("%s.log", p))
					if err != nil {
						logger.Errorf("failed to create log file (%v)", err)
						return
					}
					time.Sleep(1 * time.Minute)
					writer := io.MultiWriter(logFile, os.Stdout)
					singleLogger := pipeline.NewLoggerPipelineFactory(&log2.LogFormatter{}, writer).Make(log.LevelDebug)
					if success := runSingleTest(
						singleLogger,
						path.Join(directory, info.Name()),
						ctx,
						clientFactory,
						dockerClient,
					); success {
						if _, err := os.Create(successFile); err != nil {
							logger.Warningf("failed to create success file %s (%v)", successFile, err)
						}
					} else {
						if _, err := os.Create(failedFile); err != nil {
							logger.Warningf("failed to create failed file %s (%v)", failedFile, err)
						}
					}
					_ = logFile.Close()
				}
				return
			}()
		}
	}
}

func runSingleTest(logger log.Logger, directory string, ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient *client.Client) bool {
	logger.Infof("checking code at %s, this will take a long time...", directory)
	if err := handin.RunTests(ctx, clientFactory, dockerClient, directory, logger); err != nil {
		logger.Errorf("run failed: %v", err)
		return false
	} else {
		logger.Noticef("run successful")
		return true
	}
	return false
}
