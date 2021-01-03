package handin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/containerssh/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"

	"github.com/janoszen/exoscale-account-wiper/plugin"

	"github.com/cucumber/godog"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"

	"github.com/FH-Cloud-Computing/hand-in-automation/logger"
)

type prometheusStatus = string

const (
	prometheusStatusSuccess prometheusStatus = "success"
	//prometheusStatusError   prometheusStatus = "error"
)

type prometheusResultType = string

const (
	//prometheusResultTypeMatrix prometheusResultType = "matrix"
	prometheusResultTypeVector prometheusResultType = "vector"
	//prometheusResultTypeScalar prometheusResultType = "scalar"
	//prometheusResultTypeString prometheusResultType = "string"
)

type prometheusResult struct {
	ResultType prometheusResultType `json:"resultType"`
	Result     []prometheusMetric   `json:"result"`
}

type prometheusResponse struct {
	Status    prometheusStatus `json:"status"`
	Data      prometheusResult `json:"data"`
	ErrorType string           `json:"errorType"`
	Error     string           `json:"error"`
	Warnings  []string         `json:"warnings"`
}

type prometheusMetric struct {
	Metric prometheusInstance `json:"metric"`
}

type prometheusInstance struct {
	Instance string `json:"instance"`
}

type TestContext struct {
	ctx                       context.Context
	clientFactory             *plugin.ClientFactory
	dockerClient              *client.Client
	directory                 string
	userApiKey                string
	userApiSecret             string
	imageName                 string
	imageId                   *string
	containerID               *string
	serviceDiscoveryDirectory string
	serviceDiscoveryFile      string
	metricsPort               int
	listenPort                int
	logger                    log.Logger
	scenariosSuccessful       []string
	optionalScenariosFailed   []string
	scenariosFailed           []string
	loadDone                  bool
}

func NewTestContext(ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient *client.Client, directory string, userApiKey string, userApiSecret string, logger log.Logger) *TestContext {
	return &TestContext{
		ctx:           ctx,
		clientFactory: clientFactory,
		dockerClient:  dockerClient,
		directory:     directory,
		userApiKey:    userApiKey,
		userApiSecret: userApiSecret,
		logger:        logger,
		imageName:     "fh-hand-in-test-image",
	}
}

func (tc *TestContext) InitializeTestSuite(_ *godog.TestSuiteContext) {

}

func (tc *TestContext) InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.BeforeScenario(func(sc *godog.Scenario) {
		tc.logger.Debugf("executing scenario \"%s\"...", sc.Name)
	})
	ctx.AfterScenario(func(sc *godog.Scenario, err error) {
		tc.loadDone = true
		if err != nil {
			if strings.Contains(sc.Name, "[optional]") {
				tc.optionalScenariosFailed = append(tc.optionalScenariosFailed, sc.Name)
			} else {
				tc.scenariosFailed = append(tc.scenariosFailed, sc.Name)
			}
		} else {
			tc.scenariosSuccessful = append(tc.scenariosSuccessful, sc.Name)
		}
	})
	ctx.BeforeStep(func(st *godog.Step) {
		tc.logger.Debugf("executing step \"%s\"...", st.Text)
		tc.loadDone = false
	})
	ctx.AfterStep(func(st *godog.Step, err error) {
		if err != nil {
			tc.logger.Debugf("step failed: \"%s\" (%v)", st.Text, err)
		} else {
			tc.logger.Debugf("step successful: \"%s\"", st.Text)
		}
	})
	tempDir := os.TempDir()
	tc.serviceDiscoveryDirectory = path.Join(tempDir, "service-discovery")
	if _, err := os.Stat(tc.serviceDiscoveryDirectory); err == nil {
		if err := os.RemoveAll(tc.serviceDiscoveryDirectory); err != nil {
			tc.logger.Warningf(
				"failed to remove old service discovery directory %s (%v)",
				tc.serviceDiscoveryDirectory,
				err,
			)
		}
	}
	if _, err := os.Stat(tc.serviceDiscoveryDirectory); err != nil {
		if err := os.Mkdir(tc.serviceDiscoveryDirectory, os.ModePerm); err != nil {
			tc.logger.Warningf("failed to create service discovery data directory %s (%v)", tc.serviceDiscoveryDirectory, err)
		}
	}
	tc.serviceDiscoveryFile = path.Join(tc.serviceDiscoveryDirectory, "config.json")
	tc.metricsPort = rand.Intn(64510) + 1024
	tc.listenPort = rand.Intn(64510) + 1024

	ctx.Step(`^I have applied the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
	ctx.Step(`^I apply the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
	ctx.Step(`^I have applied the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
	ctx.Step(`^I destroy using the Terraform code$`, tc.iHaveDestroyedUsingTheTerraformCode)
	ctx.Step(`^the tfstate file should be empty$`, tc.theTfstateFileShouldBeEmpty)
	ctx.Step(`^I set the instance pool to have (\d+) instances$`, tc.iSetTheInstancePoolToHaveInstances)
	ctx.Step(`^I kill all instances in the pool$`, tc.iKillAllInstancesInThePool)
	ctx.Step(`^I should receive the answer "([^"]*)" when querying the "([^"]*)" endpoint of the NLB$`, tc.iShouldReceiveTheAnswerWhenQueryingTheEndpointOfTheNLB)
	ctx.Step(`^I wait for (\d+) instances to be present$`, tc.iWaitForInstancesToBePresent)
	ctx.Step(`^There should be (\d+) instances present$`, tc.iWaitForInstancesToBePresent)
	ctx.Step(`^I wait for no instances to be present$`, tc.iWaitForNoInstancesToBePresent)
	ctx.Step(`^one instance pool should exist$`, tc.oneInstancePoolShouldExist)
	ctx.Step(`^no instance pool should exist$`, tc.noInstancePoolShouldExist)
	ctx.Step(`^one NLB should exist$`, tc.oneNLBShouldExist)
	ctx.Step(`^no NLB should exist$`, tc.noNLBShouldExist)
	ctx.Step(`^the NLB should have one service$`, tc.theNLBShouldHaveOneService)
	ctx.Step(`^the service should listen to port (\d+)$`, tc.theServiceShouldListenToPort)
	ctx.Step(`^all backends (?:are|should be) healthy after (\d+) seconds$`, tc.allBackendsShouldBeHealthyAfterSeconds)
	ctx.Step(`^there must be a monitoring server$`, tc.thereMustBeAMonitoringServer)
	ctx.Step(`^Prometheus must be accessible on port (\d+) of the monitoring server within (\d+) seconds$`, tc.prometheusMustBeAccessible)
	ctx.Step(`^CPU metrics of all instances in the instance pool must be visible in Prometheus on port (\d+) after (\d+) seconds$`, tc.cPUMetricsOfAllInstancesInTheInstancePoolMustBeVisibleInPrometheusAfterMinutes)
	ctx.Step(`^I build the Dockerfile in the "([^"]*)" folder$`, tc.iBuildTheDockerfile)
	ctx.Step(`^I start a container from the image$`, tc.iStartAContainerFromTheImage)
	ctx.Step(`^the service discovery file must contain all instance pool IPs within (\d+) seconds$`, tc.theServiceDiscoveryFileMustContainAllInstancePoolIPsWithinSeconds)
	ctx.Step(`^I should be able to stop the container with a (\d+) exit code$`, tc.stopContainer)
	ctx.Step(`^I start hitting the NLB on ([0-9]+) parallel threads$`, tc.generateLoad)
	ctx.Step(`^there should be (\d+) instances present after (\d+) seconds$`, tc.iWaitForInstancesToBePresentAfter)
	ctx.Step(`^I send a webhook the "([^"]+)" endpoint of the container$`, tc.iSendAWebhookToTheEndpointOfTheContainer)

	ctx.AfterScenario(func(sc *godog.Scenario, err error) {
		if tc.containerID != nil {
			if err := tc.dockerClient.ContainerRemove(tc.ctx, *tc.containerID, types.ContainerRemoveOptions{
				Force: true,
			}); err != nil {
				tc.logger.Warningf("failed to remove container %s (%v)", *tc.containerID, err)
			}
			tc.containerID = nil
		}
		if _, err := os.Stat(tc.serviceDiscoveryDirectory); err != nil {
			if err := os.RemoveAll(tc.serviceDiscoveryDirectory); err != nil {
				tc.logger.Warningf("failed to remove service discovery directory (%v)", err)
			}
		}
		if tc.imageId != nil {
			if _, err := tc.dockerClient.ImageRemove(tc.ctx, *tc.imageId, types.ImageRemoveOptions{
				Force:         true,
				PruneChildren: true,
			}); err != nil {
				tc.logger.Warningf("failed to remove container image %s (%v)", *tc.imageId, err)
			}
			tc.imageId = nil
		}
	})
}

func (tc *TestContext) iHaveInitializedTerraform() error {
	tc.logger.Infof("step:\trunning terraform init...")
	err := executeTerraform(tc.ctx, tc.dockerClient, tc.directory, []string{
		"init",
		"-var", fmt.Sprintf("exoscale_key=%s", tc.userApiKey),
		"-var", fmt.Sprintf("exoscale_secret=%s", tc.userApiSecret),
	}, tc.logger)
	if err != nil {
		return err
	}
	return nil
}

func (tc *TestContext) iHaveAppliedTheTerraformCode() error {
	tc.logger.Infof("step:\trunning terraform apply...")
	err := executeTerraform(tc.ctx, tc.dockerClient, tc.directory, []string{
		"apply",
		"-var", fmt.Sprintf("exoscale_key=%s", tc.userApiKey),
		"-var", fmt.Sprintf("exoscale_secret=%s", tc.userApiSecret),
	}, tc.logger)
	if err != nil {
		return err
	}
	return nil
}

func (tc *TestContext) iHaveDestroyedUsingTheTerraformCode() error {
	tc.logger.Infof("step:\trunning terraform destroy...")
	return executeTerraform(
		tc.ctx, tc.dockerClient, tc.directory,
		[]string{
			"destroy",
			"-var", fmt.Sprintf("exoscale_key=%s", tc.userApiKey),
			"-var", fmt.Sprintf("exoscale_secret=%s", tc.userApiSecret),
		},
		tc.logger,
	)
}

func (tc *TestContext) theTfstateFileShouldBeEmpty() error {
	tc.logger.Infof("step:\tchecking if tfstate file is empty...")
	directory := tc.directory
	tfStateFile := path.Join(directory, "terraform.tfstate")
	fileContents, err := ioutil.ReadFile(tfStateFile)
	if err != nil {
		//No file means no tfstate file
		return nil
	}
	data := make(map[string]interface{})
	err = json.Unmarshal(fileContents, &data)
	if err != nil {
		//No JSON = no problem
		return nil
	}
	if resources, ok := data["resources"]; ok {
		if count := len(resources.([]interface{})); count != 0 {
			return fmt.Errorf("tfstate file still contains %d items", count)
		}
	}
	return nil
}

func (tc *TestContext) iSetTheInstancePoolToHaveInstances(instances int) error {
	tc.logger.Infof("step:\tsetting instance pool to %d instances...", instances)
	instancePool, _, err := tc.getInstancePool()
	if err != nil {
		return err
	}

	exoscaleClient := tc.clientFactory.GetExoscaleClient()
	_, err = exoscaleClient.RequestWithContext(tc.ctx, &egoscale.ScaleInstancePool{
		ID:     instancePool.ID,
		ZoneID: instancePool.ZoneID,
		Size:   instances,
	})
	if err != nil {
		return err
	}
	return nil
}

func (tc *TestContext) iKillAllInstancesInThePool() error {
	tc.logger.Infof("step:\tkilling all instances in the instance pool...")
	instancePool, _, err := tc.getInstancePool()
	if err != nil {
		return err
	}

	wg := &sync.WaitGroup{}
	exoscaleClient := tc.clientFactory.GetExoscaleClient()
	errChannel := make(chan error, len(instancePool.VirtualMachines))
	instances := 0
	for _, vm := range instancePool.VirtualMachines {
		if vm.State == "Stopped" || vm.State == "Running" || vm.State == "Starting" {
			instances++
			wg.Add(1)
			tc.logger.Debugf("killing instance %s...", vm.ID)
			go func() {
				_, err := exoscaleClient.RequestWithContext(tc.ctx, &egoscale.DestroyVirtualMachine{
					ID: vm.ID,
				})
				if err != nil {
					tc.logger.Warningf("failed to remove instance %s (%v)", vm.ID, err)
				} else {
					tc.logger.Debugf("removed instance %s", vm.ID)
				}
				wg.Done()
				errChannel <- err
			}()
		}
	}
	tc.logger.Debugf("waiting for %d instances to be removed...", instances)
	wg.Wait()
	tc.logger.Debugf("instance removal complete.")
	for i := 0; i < instances; i++ {
		err = <-errChannel
		if err != nil {
			return err
		}
	}
	return nil
}

func (tc *TestContext) iShouldReceiveTheAnswerWhenQueryingTheEndpointOfTheNLB(expectedAnswer string, endpoint string) error {
	tc.logger.Infof("step:\tquerying NLB for response...")
	nlb, _, err := tc.getNLB()
	if err != nil {
		return err
	}

	backoff := 10 * time.Second
	retries := 0
	maxRetries := 30

	for {
		retries++
		result, err := http.Get(fmt.Sprintf("http://%s%s", nlb.IPAddress, endpoint))
		if err != nil {
			if retries > maxRetries {
				return fmt.Errorf("HTTP service failed to come up (%v)", err)
			}
			tc.logger.Debugf("waiting for HTTP service to become available (%d)", retries*int(backoff/time.Second))
			time.Sleep(backoff)
			continue
		}
		body, err := ioutil.ReadAll(result.Body)
		if err != nil {
			tc.logger.Warningf("failed to read response from NLB")
			return err
		}
		if string(body) != expectedAnswer {
			return fmt.Errorf("returned answer %s did not match expected answer %s", body, expectedAnswer)
		}
		return nil
	}
}

func (tc *TestContext) iWaitForInstancesToBePresent(instances int) error {
	tc.logger.Infof("step:\twaiting for %d instances to be present...", instances)
	backoff := 10 * time.Second
	retries := 0
	retryLimit := 30
	for {
		instancePool, _, err := tc.getInstancePool()
		if err != nil {
			tc.logger.Warningf("failed to fetch instance pool (%v)", err)
			time.Sleep(backoff)
			continue
		}

		runningVMs := 0
		for _, vm := range instancePool.VirtualMachines {
			if vm.State == "Running" {
				runningVMs++
			}
		}
		if runningVMs == instances {
			tc.logger.Debugf("instances present")
			return nil
		}

		retries++
		if retries > retryLimit {
			return fmt.Errorf("invalid number of running instances (%d) for instance pool, expected %d", runningVMs, instances)
		}
		tc.logger.Debugf("waiting for %d instances to be present, currently %d (%d)", instances, runningVMs, retries*int(backoff/time.Second))
		time.Sleep(backoff)
	}
}

func (tc *TestContext) iWaitForNoInstancesToBePresent() error {
	return tc.iWaitForInstancesToBePresent(0)
}

func (tc *TestContext) oneInstancePoolShouldExist() error {
	tc.logger.Infof("step:\tchecking if exactly one instance pool exists...")
	_, _, err := tc.getInstancePool()
	return err
}

func (tc *TestContext) noInstancePoolShouldExist() error {
	tc.logger.Infof("step:\tchecking if exactly no instance pools exist...")
	exoscaleClient := tc.clientFactory.GetExoscaleClient()
	resp, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListZones{})
	if err != nil {
		return err
	}
	instancePoolCount := 0
	var instancePoolNames []string
	for _, z := range resp.(*egoscale.ListZonesResponse).Zone {
		resp, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListInstancePools{ZoneID: z.ID})
		if err != nil {
			return err
		}

		for _, i := range resp.(*egoscale.ListInstancePoolsResponse).InstancePools {
			instancePoolCount++
			instancePoolNames = append(instancePoolNames, i.Name)
		}
	}
	if instancePoolCount != 0 {
		return fmt.Errorf("invalid number of instance pools: %d", instancePoolCount)
	}
	return nil
}

func (tc *TestContext) getInstancePool() (*egoscale.InstancePool, *egoscale.Zone, error) {
	exoscaleClient := tc.clientFactory.GetExoscaleClient()
	listZonesResponse, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListZones{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list zones (%v)", err)
	}
	instancePoolCount := 0
	var instancePoolNames []string
	var instancePool *egoscale.InstancePool
	var zone *egoscale.Zone
	for _, z := range listZonesResponse.(*egoscale.ListZonesResponse).Zone {
		listInstancePoolsResponse, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListInstancePools{ZoneID: z.ID})
		if err != nil {
			return nil, nil, err
		}
		tmpZone := z

		for _, i := range listInstancePoolsResponse.(*egoscale.ListInstancePoolsResponse).InstancePools {
			tmpInstancePool := i
			instancePoolCount++
			instancePoolNames = append(instancePoolNames, i.Name)
			instancePool = &tmpInstancePool
			zone = &tmpZone
		}
	}
	if instancePoolCount != 1 {
		return nil, nil, fmt.Errorf("invalid number of Instance Pools: %d %v", instancePoolCount, instancePoolNames)
	}
	return instancePool, zone, nil
}

func (tc *TestContext) oneNLBShouldExist() error {
	tc.logger.Infof("step:\tchecking if exactly one NLB exists...")
	_, _, err := tc.getNLB()
	return err
}

func (tc *TestContext) noNLBShouldExist() error {
	tc.logger.Infof("step:\tchecking if no NLBs exist...")
	exoscaleClient := tc.clientFactory.GetExoscaleClient()
	resp, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListZones{})
	if err != nil {
		return fmt.Errorf("failed to list zones (%v)", err)
	}
	nlbCount := 0
	var nlbNames []string
	for _, z := range resp.(*egoscale.ListZonesResponse).Zone {
		v2Ctx := tc.clientFactory.GetExoscaleV2Context(z.Name, tc.ctx)
		list, err := exoscaleClient.ListNetworkLoadBalancers(v2Ctx, z.Name)
		if err != nil {
			return fmt.Errorf("unable to list Network Load Balancers in zone %s: %v", z.Name, err)
		}

		for _, n := range list {
			nlbCount++
			nlbNames = append(nlbNames, n.Name)
		}
	}
	if nlbCount != 0 {
		return fmt.Errorf("invalid number of NLB's: %d", nlbCount)
	}
	return nil
}

func (tc *TestContext) getNLB() (*egoscale.NetworkLoadBalancer, *egoscale.Zone, error) {
	exoscaleClient := tc.clientFactory.GetExoscaleClient()
	resp, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListZones{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list zones (%v)", err)
	}
	nlbCount := 0
	var nlbNames []string
	var nlb *egoscale.NetworkLoadBalancer
	var zone *egoscale.Zone
	for _, z := range resp.(*egoscale.ListZonesResponse).Zone {
		v2Ctx := tc.clientFactory.GetExoscaleV2Context(z.Name, tc.ctx)
		list, err := exoscaleClient.ListNetworkLoadBalancers(v2Ctx, z.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to list Network Load Balancers in zone %s: %v", z.Name, err)
		}

		for _, n := range list {
			nlbCount++
			nlbNames = append(nlbNames, n.Name)
			nlb = n
			zone = &z
		}
	}
	if nlbCount != 1 {
		return nil, nil, fmt.Errorf("invalid number of Network Load Balancers: %d %v", nlbCount, nlbNames)
	}
	return nlb, zone, nil
}

func (tc *TestContext) theNLBShouldHaveOneService() error {
	tc.logger.Infof("step:\tchecking if the NLB has exactly one service...")
	_, _, _, err := tc.getNLBService()
	return err
}

func (tc *TestContext) getNLBService() (*egoscale.NetworkLoadBalancerService, *egoscale.NetworkLoadBalancer, *egoscale.Zone, error) {
	nlb, zone, err := tc.getNLB()
	if err != nil {
		return nil, nil, nil, err
	}
	if len(nlb.Services) != 1 {
		return nil, nlb, zone, fmt.Errorf("invalid number of services on Load Balancer: %d, ", len(nlb.Services))
	}
	return nlb.Services[0], nlb, zone, nil
}

func (tc *TestContext) theServiceShouldListenToPort(port int) error {
	tc.logger.Infof("step:\tchecking if the service listens to port %d...", port)
	service, _, _, err := tc.getNLBService()
	if err != nil {
		return err
	}
	if int(service.Port) != port {
		return fmt.Errorf("invalid listen port: %d", service.Port)
	}
	return nil
}

func (tc *TestContext) allBackendsShouldBeHealthyAfterSeconds(seconds int) error {
	tc.logger.Infof("step:\twaiting %d seconds for all backends to be healthy...", seconds)
	tries := 0
	var backoff = 10 * time.Second
	for {
		service, _, _, err := tc.getNLBService()
		if err != nil {
			return err
		}
		healthCheckStatuses := service.HealthcheckStatus
		unhealthyBackends := 0
		for _, status := range healthCheckStatuses {
			if status.Status != "success" {
				unhealthyBackends++
			}
		}
		if unhealthyBackends != 0 {
			tries++
			if tries > seconds/int(backoff/time.Second) {
				return fmt.Errorf("%d out of %d backends for the service are unhealthy", unhealthyBackends, len(service.HealthcheckStatus))
			}
			tc.logger.Debugf("%d out of %d backends for the service are unhealthy (%d)", unhealthyBackends, len(service.HealthcheckStatus), tries*int(backoff/time.Second))
			time.Sleep(backoff)
		} else {
			return nil
		}
	}
}

func (tc *TestContext) getMonitoringServerIp() (string, error) {
	exoscaleClient := tc.clientFactory.GetExoscaleClient()

	vm := &egoscale.VirtualMachine{}
	vms, err := exoscaleClient.List(vm)
	if err != nil {
		return "", err
	}

	var ips = map[string]bool{}
	for _, key := range vms {
		vm := key.(*egoscale.VirtualMachine)
		for _, nic := range vm.Nic {
			if nic.IsDefault {
				ips[nic.IPAddress.String()] = true
			}
		}
	}

	instancePool, _, err := tc.getInstancePool()
	foundIp := ""
	if err == nil {
		instancePoolIps := map[string]bool{}
		for _, vm := range instancePool.VirtualMachines {
			for _, nic := range vm.Nic {
				if nic.IsDefault {
					instancePoolIps[nic.IPAddress.String()] = true
				}
			}
		}
		for ip := range ips {
			if _, ok := instancePoolIps[ip]; !ok {
				if foundIp != "" {
					return "", fmt.Errorf("too many non-instancepool VM's running: %d", len(ips))
				}
				foundIp = ip
			}
		}
	} else {
		if len(ips) != 1 {
			return "", fmt.Errorf("too many non-instancepool VM's running: %d", len(ips))
		}
	}
	if foundIp == "" {
		return "", fmt.Errorf("monitoring server not running")
	}
	return foundIp, nil
}

func (tc *TestContext) thereMustBeAMonitoringServer() error {
	tc.logger.Debugf("step:\tchecking for one monitoring server to be present...")
	_, err := tc.getMonitoringServerIp()
	return err
}

func (tc *TestContext) prometheusMustBeAccessible(port int, seconds int) error {
	tc.logger.Infof("step:\tchecking if Prometheus is accessible on port %d after %d seconds...", port, seconds)
	monitoringIp, err := tc.getMonitoringServerIp()
	if err != nil {
		return err
	}
	query := "sum by (instance) (rate(node_cpu_seconds_total{mode!=\"idle\"}[1m])) / sum by (instance) (rate(node_cpu_seconds_total[1m]))"
	monitoringUrl := fmt.Sprintf("http://%s:%d/api/v1/query?query=%s", monitoringIp, port, url.QueryEscape(query))
	retry := 0
	backoff := 10
	for {
		retry++
		if retry*backoff > seconds {
			return fmt.Errorf("timeout while waiting for Prometheus to come up")
		}
		time.Sleep(time.Duration(backoff) * time.Second)
		response, err := http.Get(monitoringUrl)
		if err != nil {
			tc.logger.Debugf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		if response.StatusCode != 200 {
			tc.logger.Debugf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			tc.logger.Debugf("failed to read response from %s (%v)", monitoringUrl, err)
			continue
		}
		data := &prometheusResponse{}
		err = json.Unmarshal(body, data)
		if err != nil {
			tc.logger.Debugf("failed to decode JSON from Prometheus (%v)", err)
			continue
		}
		if data.Status != prometheusStatusSuccess {
			tc.logger.Debugf("error from Prometheus during query (%v)", data.Error)
			continue
		}
		if data.Data.ResultType != prometheusResultTypeVector {
			tc.logger.Warningf("unexpected response type from Prometheus (%v)", data.Data.ResultType)
			continue
		}
		tc.logger.Debugf("Prometheus is up")
		return nil
	}
}

func (tc *TestContext) cPUMetricsOfAllInstancesInTheInstancePoolMustBeVisibleInPrometheusAfterMinutes(port int, seconds int) error {
	tc.logger.Infof("step:\tchecking if all instances in the pool have their CPU metrics visible in Prometheus on port %d after %d seconds...", port, seconds)
	monitoringIp, err := tc.getMonitoringServerIp()
	if err != nil {
		return err
	}
	query := "sum by (instance) (rate(node_cpu_seconds_total{mode!=\"idle\"}[1m])) / sum by (instance) (rate(node_cpu_seconds_total[1m]))"
	monitoringUrl := fmt.Sprintf("http://%s:%d/api/v1/query?query=%s", monitoringIp, port, url.QueryEscape(query))
	retry := 0
	backoff := 10
	for {
		retry++
		if retry*backoff > seconds {
			return fmt.Errorf("timeout while waiting for Prometheus to come up")
		}
		time.Sleep(time.Duration(backoff) * time.Second)
		response, err := http.Get(monitoringUrl)
		if err != nil {
			tc.logger.Debugf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		if response.StatusCode != 200 {
			tc.logger.Debugf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			tc.logger.Debugf("failed to read response from %s (%v)", monitoringUrl, err)
			continue
		}
		data := &prometheusResponse{}
		err = json.Unmarshal(body, data)
		if err != nil {
			tc.logger.Debugf("failed to decode JSON from Prometheus (%v)", err)
			continue
		}
		if data.Status != prometheusStatusSuccess {
			tc.logger.Debugf("error from Prometheus during query (%v)", data.Error)
			continue
		}
		if data.Data.ResultType != prometheusResultTypeVector {
			tc.logger.Debugf("unexpected response type from Prometheus (%v)", data.Data.ResultType)
			continue
		}
		instancePool, _, err := tc.getInstancePool()
		if err != nil {
			tc.logger.Debugf("failed to get instance pool (%v)", data.Data.ResultType)
			continue
		}
		metricIPs := map[string]bool{}
		var expectedIPs []string
		for _, record := range data.Data.Result {
			parts := strings.SplitN(record.Metric.Instance, ":", 2)
			metricIPs[parts[0]] = true
			expectedIPs = append(expectedIPs, parts[0])
		}
		tc.logger.Debugf("found the following IPs in the Prometheus output: %v", expectedIPs)
		var foundIPs []string
		allIpsFound := true
		for _, vm := range instancePool.VirtualMachines {
			for _, nic := range vm.Nic {
				if nic.IsDefault {
					foundIPs = append(foundIPs, nic.IPAddress.String())
					if _, ok := metricIPs[nic.IPAddress.String()]; !ok {
						allIpsFound = false
					}
				}
			}
		}
		tc.logger.Debugf("found following IPs in the instance pool: %v", foundIPs)
		if !allIpsFound {
			tc.logger.Debugf("not all IP's are in Prometheus")
			continue
		}

		return nil
	}
}

func (tc *TestContext) iBuildTheDockerfile(folder string) error {
	tc.logger.Infof("step:\tbuilding the Dockerfile in the %s directory...", folder)
	directory := path.Join(tc.directory, folder)
	stat, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("%s is not a directory", folder)
	}
	dockerfile := path.Join(directory, "Dockerfile")
	stat, err = os.Stat(dockerfile)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return fmt.Errorf("%s is a directory", dockerfile)
	}

	reader, writer := io.Pipe()
	defer func() {
		_ = writer.Close()
	}()
	defer func() {
		_ = reader.Close()
	}()

	go func() {
		if err := tarDirectory(directory, writer); err != nil {
			tc.logger.Warningf("failed to build tar for Docker build (%v)", err)
		}
		if err := writer.CloseWithError(io.EOF); err != nil {
			tc.logger.Warningf("failed to close writer (%v)", err)
		}
	}()

	tags := []string{tc.imageName}

	response, err := tc.dockerClient.ImageBuild(tc.ctx, reader, types.ImageBuildOptions{
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to build image (%v)", err)
	}
	defer func() { _ = response.Body.Close() }()

	tc.imageId, err = DockerToLogger(response.Body, tc.logger)
	if err != nil {
		return err
	}

	tc.logger.Debugf("image build completed successfully")
	return nil
}

func (tc *TestContext) iStartAContainerFromTheImage() error {
	tc.logger.Infof("step:\tstarting container...")
	if tc.imageId == nil {
		return fmt.Errorf("container image has not been built")
	}
	if tc.containerID != nil {
		err := tc.dockerClient.ContainerRemove(context.Background(), *tc.containerID, types.ContainerRemoveOptions{
			Force: true,
		})
		if err != nil {
			return fmt.Errorf("failed to remove previous container (%w)", err)
		}
	}
	instancePool, zone, err := tc.getInstancePool()
	if err != nil {
		return err
	}
	containerCreate, err := tc.dockerClient.ContainerCreate(
		tc.ctx,
		&container.Config{
			Env: []string{
				fmt.Sprintf("EXOSCALE_KEY=%s", tc.userApiKey),
				fmt.Sprintf("EXOSCALE_SECRET=%s", tc.userApiSecret),
				fmt.Sprintf("EXOSCALE_ZONE=%s", zone.Name),
				fmt.Sprintf("EXOSCALE_ZONE_ID=%s", zone.ID),
				fmt.Sprintf("EXOSCALE_INSTANCEPOOL_ID=%s", instancePool.ID),
				fmt.Sprintf("TARGET_PORT=%d", tc.metricsPort),
				fmt.Sprintf("LISTEN_PORT=%d", tc.listenPort),
			},
			Image:        *tc.imageId,
			AttachStdout: true,
			AttachStderr: true,
			ExposedPorts: nat.PortSet{
				nat.Port(fmt.Sprintf("%d/tcp", tc.listenPort)): struct{}{},
			},
		},
		&container.HostConfig{
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: tc.serviceDiscoveryDirectory,
					Target: "/srv/service-discovery",
				},
			},
			PortBindings: nat.PortMap{
				nat.Port(fmt.Sprintf("%d/tcp", tc.listenPort)): []nat.PortBinding{
					{
						HostIP:   "127.0.0.1",
						HostPort: fmt.Sprintf("%d", tc.listenPort),
					},
				},
			},
		},
		&network.NetworkingConfig{},
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to create container from image (%v)", err)
	}
	id := containerCreate.ID
	tc.containerID = &id

	startOptions := types.ContainerStartOptions{}
	err = tc.dockerClient.ContainerStart(tc.ctx, *tc.containerID, startOptions)
	if err != nil {
		return fmt.Errorf("failed to start container (%v)", err)
	}

	containerOutput, err := tc.dockerClient.ContainerLogs(tc.ctx, *tc.containerID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to stream container logs (%v)", err)
	}

	go func() {
		logWriter := &logger.LogWriter{
			Logger: tc.logger,
		}
		_, err = stdcopy.StdCopy(logWriter, logWriter, containerOutput)
		if err != nil {
			tc.logger.Warningf("failed to stream logs from Docker (%v)", err)
			return
		}
	}()

	return nil
}

type ServiceDiscoveryRecord struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}
type ServiceDiscoveryFile []ServiceDiscoveryRecord

func (tc *TestContext) theServiceDiscoveryFileMustContainAllInstancePoolIPsWithinSeconds(seconds int64) error {
	tc.logger.Infof("step:\tchecking if all IPs in the instance pool are present in the service discovery file...")
	startTime := time.Now()
	maxEndTime := startTime.Add(time.Duration(seconds) * time.Second)
	for {
		if time.Now().After(maxEndTime) {
			tc.logger.Warningf("no or invalid service discovery found within %d seconds, timeout", seconds)
			return fmt.Errorf("timeout while waiting for service discovery")
		}
		tc.logger.Debugf("running checks on service discovery file %s...", tc.serviceDiscoveryFile)
		tc.logger.Debugf("checking if the service discovery file is present at %s...", tc.serviceDiscoveryFile)
		_, err := os.Stat(tc.serviceDiscoveryFile)
		if err != nil {
			tc.logger.Debugf("service discovery file %s does not exist yet", tc.serviceDiscoveryFile)
			time.Sleep(10 * time.Second)
			continue
		}
		tc.logger.Debugf("opening service discovery file...")
		fh, err := os.Open(tc.serviceDiscoveryFile)
		if err != nil {
			tc.logger.Debugf("failed to open service discovery file %s (%v)", tc.serviceDiscoveryFile, err)
			time.Sleep(10 * time.Second)
			continue
		}
		tc.logger.Debugf("decoding JSON contents...")
		decoder := json.NewDecoder(fh)
		decoded := &ServiceDiscoveryFile{}
		if err := decoder.Decode(decoded); err != nil {
			tc.logger.Debugf("failed to decode service discovery file %s (%v)", tc.serviceDiscoveryFile, err)
			time.Sleep(10 * time.Second)
			continue
		}

		var foundTargets []string
		var decodedTargets = make(map[string]bool)
		for _, record := range *decoded {
			for _, entry := range record.Targets {
				foundTargets = append(foundTargets, entry)
				decodedTargets[entry] = true
			}
		}

		instancePoolIps, err := tc.getInstancePoolIps()
		if err != nil {
			tc.logger.Warningf("failed to fetch IPs from instance pool (%v)", err)
			continue
		}

		var expectedTargets []string
		for _, ip := range instancePoolIps {
			expectedTargets = append(expectedTargets, fmt.Sprintf("%s:%d", ip.String(), tc.metricsPort))
		}

		sort.SliceStable(foundTargets, func(i, j int) bool {
			return foundTargets[i] > foundTargets[j]
		})
		sort.SliceStable(expectedTargets, func(i, j int) bool {
			return expectedTargets[i] > expectedTargets[j]
		})

		tc.logger.Debugf("expecting the following targets: %v", expectedTargets)
		tc.logger.Debugf("found the following targets: %v", foundTargets)

		if len(decodedTargets) != len(instancePoolIps) {
			tc.logger.Debugf("number targets in the service discovery file (%d) does not match IPs in the instance pool (%d)", len(decodedTargets), len(instancePoolIps))
			time.Sleep(10 * time.Second)
			continue
		}

		allIpsPresent := true
		for _, ip := range instancePoolIps {
			target := fmt.Sprintf("%s:%d", ip.String(), tc.metricsPort)
			if _, ok := decodedTargets[target]; !ok {
				tc.logger.Debugf("expected target %s is not in the service discovery file", target)
				allIpsPresent = false
			}
		}
		if !allIpsPresent {
			tc.logger.Debugf("not all IPs are present, retrying in 10 seconds...")
			time.Sleep(10 * time.Second)
			continue
		}
		tc.logger.Debugf("all IPs are present in the service discovery file")
		return nil
	}
}

func (tc *TestContext) getInstancePoolIps() ([]net.IP, error) {
	instancePool, _, err := tc.getInstancePool()
	if err != nil {
		return []net.IP{}, err
	}

	var ips []net.IP
	for _, vm := range instancePool.VirtualMachines {
		var ip net.IP
		for _, nic := range vm.Nic {
			if nic.IPAddress != nil {
				ip = nic.IPAddress
			}
		}
		if ip == nil {
			return []net.IP{}, fmt.Errorf("failed to fetch IP for VM %s", vm.ID)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

func (tc *TestContext) stopContainer(exitCode int) error {
	tc.logger.Infof("step:\tchecking if the container can be normally stopped...")
	if tc.containerID == nil {
		return fmt.Errorf("container not running")
	}
	inspect, err := tc.dockerClient.ContainerInspect(tc.ctx, *tc.containerID)
	if err != nil {
		tc.logger.Warningf("failed to inspect container (%v)", err)
		return err
	}
	if inspect.State == nil || !inspect.State.Running {
		tc.logger.Warningf("container is not running")
		return fmt.Errorf("container is not running")
	}
	if err := tc.dockerClient.ContainerStop(tc.ctx, *tc.containerID, nil); err != nil {
		tc.logger.Warningf("failed to stop container (%v)", err)
		return fmt.Errorf("failed to stop container (%v)", err)
	}
	inspect, err = tc.dockerClient.ContainerInspect(tc.ctx, *tc.containerID)
	if err != nil {
		tc.logger.Warningf("failed to inspect container (%v)", err)
		return err
	}
	if inspect.State.Running != false {
		tc.logger.Warningf("container still running")
		return fmt.Errorf("container still running")
	}
	if inspect.State.ExitCode != exitCode {
		tc.logger.Warningf("container exited with unexpected exit code (%d)", inspect.State.ExitCode)
		return fmt.Errorf("container exited with unexpected exit code (%d)", inspect.State.ExitCode)
	}
	return nil
}

func (tc *TestContext) generateLoad(threads int) error {
	tc.loadDone = false
	nlb, _, err := tc.getNLB()
	if err != nil {
		return err
	}
	ip := nlb.IPAddress
	for i := 0; i < threads; i++ {
		threadNumber := i
		go func() {
			for {
				if tc.loadDone {
					return
				}
				address := fmt.Sprintf("http://%s/load", ip.String())
				tc.logger.Noticef("Sending HTTP request to %s on thread %d...", address, threadNumber)
				_, err = http.Get(address)
				if err != nil {
					tc.logger.Noticef("Failed to query NLB %s on thread %d (%v). This is normal, we are overloading the NLB.", address, threadNumber)
				}
			}
		}()
	}
	return nil
}

func (tc *TestContext) iWaitForInstancesToBePresentAfter(instances int, timeout int) error {
	tc.logger.Infof("step:\twaiting for %d instances to be present after %d seconds...", instances, timeout)
	backoff := 10 * time.Second
	retries := 0
	retryLimit := timeout / 10
	for {
		instancePool, _, err := tc.getInstancePool()
		if err != nil {
			tc.logger.Warningf("failed to fetch instance pool (%v)", err)
			time.Sleep(backoff)
			continue
		}

		runningVMs := 0
		for _, vm := range instancePool.VirtualMachines {
			if vm.State == "Running" {
				runningVMs++
			}
		}
		if runningVMs == instances {
			tc.logger.Debugf("instances present")
			return nil
		}

		retries++
		if retries > retryLimit {
			return fmt.Errorf("invalid number of running instances (%d) for instance pool, expected %d", runningVMs, instances)
		}
		tc.logger.Debugf("waiting for %d instances to be present, currently %d (%d)", instances, runningVMs, retries*int(backoff/time.Second))
		time.Sleep(backoff)
	}
}

func (tc *TestContext) iSendAWebhookToTheEndpointOfTheContainer(endpoint string) error {
	tc.logger.Infof("step:\tsending a webhook to the %s endpoint of the container", endpoint)
	if tc.containerID == nil {
		return fmt.Errorf("container not running")
	}
	tries := 0
	for {
		if tries > 10 {
			return fmt.Errorf("too many autoscaler failures, giving up")
		}
		tries++
		response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", tc.listenPort, endpoint))
		if err != nil {
			tc.logger.Debugf("webhook endpoint responded with error (%v)", err)
			continue
		}
		if response.StatusCode < 200 || response.StatusCode > 299 {
			tc.logger.Debugf("webhook endpoint responded with an invalid status code: %d", response.StatusCode)
			continue
		}
		break
	}
	tc.logger.Debugf("Webhook successful")
	return nil
}
