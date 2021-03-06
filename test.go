package handin

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

	log2 "github.com/FH-Cloud-Computing/hand-in-automation/logger"
)

func RunTests(ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient *client.Client, directory string, logger log.Logger) error {
	goLogger := &log2.LogWriter{Prefix: "result:", Logger: logger}
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
		// Error logs are printed within wipeAccount
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

	logger.Infof("running terraform init...")
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
		StopOnFailure: false,
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
	_ = s.Run()
	os.Stdout = realStdout
	lines := strings.Split(buf.String(), "\n")
	for _, line := range lines {
		logger.Notice(line)
	}

	if len(tc.scenariosFailed) > 0 {
		logger.Errorf("scenarios failed: %v", tc.scenariosFailed)
	}
	if len(tc.scenariosSuccessful) > 0 {
		logger.Noticef("scenarios successful: %v", tc.scenariosSuccessful)
	}
	if len(tc.optionalScenariosFailed) > 0 {
		logger.Warningf("optional scenarios failed: %v", tc.optionalScenariosFailed)
	}

	if len(tc.scenariosFailed) > 0 {
		return fmt.Errorf("%d required scenarios failed", len(tc.scenariosFailed))
	}

	return nil
}
