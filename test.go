package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	goLog "log"
	"os"
	"path"
	"strings"

	"github.com/containerssh/log"
	"github.com/cucumber/godog"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/janoszen/exoscale-account-wiper/plugin"
)

func runTests(ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient *client.Client, directory string, logger log.Logger) error {
	goLogger := &logWriter{logger: logger}
	goLog.SetOutput(goLogger)
	originalFlags := goLog.Flags()
	goLog.SetFlags(0)
	defer goLog.SetFlags(originalFlags)
	defer goLog.SetOutput(os.Stdout)

	err := wipeAccount(ctx, clientFactory, logger)
	if err != nil {
		return err
	}
	defer func() {
		//Error logs are printed within wipeAccount
		_ = wipeAccount(ctx, clientFactory, logger)
	}()

	tfStateFile := path.Join(directory, "terraform.tfstate")
	_, err = os.Stat(tfStateFile)
	if err == nil {
		err := os.Remove(tfStateFile)
		if err != nil {
			logger.Warningf("failed to remove tfstate file (%v)", err)
			return err
		}
	}

	exoscaleClient := clientFactory.GetExoscaleClient()
	logger.Infof("creating Exoscale API key for Terraform...")
	resp, err := exoscaleClient.RequestWithContext(ctx, &egoscale.CreateAPIKey{
		Name:       "terraform",
		Operations: "compute/*,sos/*",
	})
	if err != nil {
		logger.Warningf("failed to create restricted API key (%v)", err)
		return err
	}
	userApiKey := resp.(*egoscale.APIKey).Key
	userApiSecret := resp.(*egoscale.APIKey).Secret

	err = executeTerraform(ctx, dockerClient, directory, []string{
		"init",
		"-var", fmt.Sprintf("exoscale_key=%s", userApiKey),
		"-var", fmt.Sprintf("exoscale_secret=%s", userApiSecret),
	}, logger)
	if err != nil {
		logger.Warningf("failed to initialize Terraform (%v)", err)
		return err
	}

	logger.Infof("executing tests...")
	tc := NewTestContext(ctx, clientFactory, dockerClient, directory, userApiKey, userApiSecret, logger)
	var opts = godog.Options{
		Format:        "progress",
		StopOnFailure: true,
		Strict:        true,
		Concurrency:   1,
	}

	s := &godog.TestSuite{
		Name:                 "Project Work",
		TestSuiteInitializer: tc.InitializeTestSuite,
		ScenarioInitializer:  tc.InitializeScenario,
		Options:              &opts,
	}

	realStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
	}()
	status := s.Run()
	os.Stdout = realStdout
	lines := strings.Split(buf.String(), "\n")
	for _, line := range lines {
		logger.Notice(line)
	}

	if status != 0 {
		return fmt.Errorf("tests failed with status %d", status)
	}

	return nil
}
