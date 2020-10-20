package main

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"path"
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

	files, err := ioutil.ReadDir(directory)
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		projectDirectory := path.Join(directory, f.Name())
		logFileName := path.Join(projectDirectory, "output.log")
		log.Printf("Checking code at %s...", projectDirectory)
		successFile := path.Join(projectDirectory, "success")
		_, err = os.Stat(successFile)
		if err == nil {
			log.Printf("Submission already successful, skipping...")
			continue
		}
		logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			log.Fatalf("Failed to open log file (%v)", err)
		}
		log.SetOutput(logFile)
		log.Printf("Checking code at %s...", projectDirectory)
		err = runTests(ctx, clientFactory, dockerClient, projectDirectory, logFile)
		if err != nil {
			log.Printf("Run failed: %v", err)
			log.SetOutput(os.Stdout)
			log.Printf("Run failed: %v", err)
		} else {
			log.Printf("Run successful")
			_, err := os.Create(successFile)
			if err != nil {
				log.Printf("Failed to create success file at %s (%v)", successFile, err)
			}
			log.SetOutput(os.Stdout)
			log.Printf("Run successful")
		}
	}
}

