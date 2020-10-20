package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/janoszen/exoscale-account-wiper/plugin"
)

func runTests(ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient *client.Client, directory string, logFile *os.File) error {
	err := wipeAccount(ctx, clientFactory)
	if err != nil {
		return err
	}
	defer func() {
		//Error logs are printed within wipeAccount
		_ = wipeAccount(ctx, clientFactory)
	}()

	tfStateFile := path.Join(directory, "terraform.tfstate")
	_, err = os.Stat(tfStateFile)
	if err == nil {
		err := os.Remove(tfStateFile)
		if err != nil {
			log.Printf("failed to remove tfstate file (%v)", err)
			return err
		}
	}

	exoscaleClient := clientFactory.GetExoscaleClient()
	log.Printf("creating Exoscale API key for Terraform...")
	resp, err := exoscaleClient.RequestWithContext(ctx, &egoscale.CreateAPIKey{
		Name:       "terraform",
		Operations: "compute/*,sos/*",
	})
	if err != nil {
		log.Printf("failed to create restricted API key (%v)", err)
		return err
	}
	userApiKey := resp.(*egoscale.APIKey).Key
	userApiSecret := resp.(*egoscale.APIKey).Secret

	err = executeTerraform(ctx, dockerClient, directory, []string{
		"init",
		"-var", fmt.Sprintf("exoscale_key=%s", userApiKey),
		"-var", fmt.Sprintf("exoscale_secret=%s", userApiSecret),
	}, logFile)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	tc := NewTestContext(ctx, clientFactory, dockerClient, directory, userApiKey, userApiSecret, logFile)
	var opts = godog.Options{
		Format: "progress",
		Output: colors.Colored(buf),
	}

	s := &godog.TestSuite{
		Name:                 "Project Work",
		TestSuiteInitializer: tc.InitializeTestSuite,
		ScenarioInitializer:  tc.InitializeScenario,
		Options:              &opts,
	}
	status := s.Run()
	println(buf.String())
	err = ioutil.WriteFile(path.Join(directory, "test.log"), []byte(buf.String()), 0644)
	if err != nil {
		log.Printf("failed to write test log file (%v)", err)
	}

	if status != 0 {
		return fmt.Errorf("tests failed with status %d", status)
	}

	return nil
}
