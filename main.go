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

func executeTerraform(ctx context.Context, dockerClient *client.Client, directory string, command []string, logFile *os.File) error {
	log.Printf("executing Terraform operation %s...", command[0])

	log.Printf("pulling Terraform image...")
	pullResult, err := dockerClient.ImagePull(ctx, "janoszen/terraform", types.ImagePullOptions{})
	if err != nil {
		log.Printf("failed to pull Terraform container image (%v)", err)
		return err
	}
	_, err = io.Copy(logFile, pullResult)
	if err != nil {
		log.Printf("failed to stream pull results from Terraform image (%v)", err)
		return err
	}

	log.Printf("creating Terraform container...")
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
		"",
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
				Force: true,
			},
		)
		if err != nil {
			log.Printf("failed to remove container %s (%v)", terraformContainer.ID, err)
		} else {
			log.Printf("removed container %s.", terraformContainer.ID)
		}
	}()
	log.Printf("starting Terraform container...")
	err = dockerClient.ContainerStart(ctx, terraformContainer.ID, types.ContainerStartOptions{})
	if err != nil {
		log.Printf("failed to start Terraform container (%v)", err)
		return err
	}
	log.Printf("streaming logs from Terraform container...")
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
	logBuffer := bytes.NewBuffer(nil)
	_, err = stdcopy.StdCopy(logBuffer, logBuffer, containerOutput)
	if err != nil {
		log.Printf("failed to stream logs from Terraform container (%v)", err)
		return err
	}

	logs := logBuffer.Bytes()
	_, err = logFile.Write(logs)
	if err != nil {
		log.Printf("Failed to copy Docker buffer to log file (%v)", err)
		return err
	}

	inspect, err := dockerClient.ContainerInspect(ctx, terraformContainer.ID)
	if err != nil {
		log.Printf("failed to inspect Terraform container (%v)", err)
		return err
	}

	if inspect.State.ExitCode != 0 {
		log.Printf("terraform %s failed:\n---\n%s\n---\n", command[0], logs)
		return fmt.Errorf("terraform %s failed:\n---\n%s\n---\n", command[0], logBuffer.String())
	}

	log.Printf("Terraform operation %s complete.", command[0])
	return nil
}

func wipeAccount(ctx context.Context, clientFactory *plugin.ClientFactory) error {
	log.Printf("wiping Exoscale account...")
	wiper := factory.CreateRegistry()
	err := wiper.SetConfiguration(map[string]string{"iam-exclude-self": "1"}, false)
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
