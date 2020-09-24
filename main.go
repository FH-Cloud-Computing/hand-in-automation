package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/docker/docker/pkg/stdcopy"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"runtime"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/janoszen/exoscale-account-wiper/factory"
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

	err = runTests(ctx, clientFactory, dockerClient, directory)
	if err != nil {
		defer os.Exit(1)
		runtime.Goexit()
	}
}

func runTests(ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient  *client.Client, directory string) error {
	err := wipeAccount(ctx, clientFactory)
	if err != nil {
		return err
	}
	defer func() {
		//Error logs are printed within wipeAccount
		_ = wipeAccount(ctx, clientFactory)
	}()

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

	err = executeTerraform(
		ctx,
		dockerClient,
		directory,
		[]string{
			"init",
			"-var", fmt.Sprintf("exoscale_key=%s", userApiKey),
			"-var", fmt.Sprintf("exoscale_secret=%s", userApiSecret),
		},
	)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	tc := NewTestContext(ctx, clientFactory, dockerClient, directory, userApiKey, userApiSecret)
	var opts = godog.Options{
		Format:              "progress",
		Output:              colors.Colored(buf),
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

	err = executeTerraform(
		ctx,
		dockerClient,
		directory,
		[]string{
			"destroy",
			"-var", fmt.Sprintf("exoscale_key=%s", userApiKey),
			"-var", fmt.Sprintf("exoscale_secret=%s", userApiSecret),
		},
	)
	if err != nil {
		return err
	}

	if status != 0 {
		return fmt.Errorf("tests failed with status %d", status)
	}

	return nil
}

func executeTerraform(ctx context.Context, dockerClient *client.Client, directory string, command []string) error {
	log.Printf("executing Terraform operation %s...", command[0])

	log.Printf("pulling Terraform image...", )
	pullResult, err := dockerClient.ImagePull(ctx, "janoszen/terraform", types.ImagePullOptions{})
	if err != nil {
		log.Printf("failed to pull Terraform container image (%v)", err)
		return err
	}
	_, err = io.Copy(os.Stdout, pullResult)
	if err != nil {
		log.Printf("failed to stream pull results from Terraform image (%v)", err)
		return err
	}

	log.Printf("creating Terraform container...", )
	terraformContainer, err := dockerClient.ContainerCreate(
		ctx,
		&container.Config{
			Env:   nil,
			Cmd:   command,
			Image: "janoszen/terraform",
		},
		&container.HostConfig{
			Mounts: []mount.Mount{
				{
					Type:   "bind",
					Source: directory,
					Target: "/terraform",
				},
			},
		},
		&network.NetworkingConfig{},
		"execute",
	)
	if err != nil {
		log.Printf("failed to create Terraform container (%v)", err)
		return err
	}
	defer func() {
		log.Printf("removing container %s...", terraformContainer.ID)
		err := dockerClient.ContainerRemove(
			ctx,
			terraformContainer.ID,
			types.ContainerRemoveOptions{
				Force:         true,
			},
		)
		if err != nil {
			log.Printf("failed to remove container %s (%v)", terraformContainer.ID, err)
		} else {
			log.Printf("removed container %s.", terraformContainer.ID)
		}
	}()
	log.Printf("starting Terraform container...", )
	err = dockerClient.ContainerStart(ctx, terraformContainer.ID, types.ContainerStartOptions{})
	if err != nil {
		log.Printf("failed to start Terraform container (%v)", err)
		return err
	}
	log.Printf("streaming logs from Terraform container...", )
	containerOutput, err := dockerClient.ContainerLogs(ctx, terraformContainer.ID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		log.Printf("failed to stream logs from Terraform container (%v)", err)
		return err
	}
	defer func() {
		_ = containerOutput.Close()
	}()
	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, containerOutput)
	if err != nil {
		log.Printf("failed to stream logs from Terraform container (%v)", err)
		return err
	}

	inspect, err := dockerClient.ContainerInspect(ctx, terraformContainer.ID)
	if err != nil {
		log.Printf("failed to inspect Terraform container (%v)", err)
		return err
	}

	if inspect.State.ExitCode != 0 {
		log.Printf("Terraform container exited with non-zero exit code (%d)", inspect.State.ExitCode)
		return err
	}

	log.Printf("Terraform operation %s complete.", command[0])
	return nil
}

func wipeAccount(ctx context.Context, clientFactory *plugin.ClientFactory) error {
	log.Printf("wiping Exoscale account...")
	wiper := factory.CreateRegistry()
	err := wiper.SetConfiguration(map[string]string{"iam-exclude-self":"1"}, false)
	if err != nil {
		return err
	}
	err = wiper.Run(clientFactory, ctx)
	if err != nil {
		log.Printf("failed to wipe Exoscale account (%v).", err)
		return err
	}
	log.Printf("wiped Exoscale account.")
	return nil
}


