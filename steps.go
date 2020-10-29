package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/janoszen/exoscale-account-wiper/plugin"

	"github.com/cucumber/godog"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
)

type prometheusStatus = string

const (
	prometheusStatusSuccess prometheusStatus = "success"
	prometheusStatusError   prometheusStatus = "error"
)

type prometheusResultType = string

const (
	prometheusResultTypeMatrix prometheusResultType = "matrix"
	prometheusResultTypeVector prometheusResultType = "vector"
	prometheusResultTypeScalar prometheusResultType = "scalar"
	prometheusResultTypeString prometheusResultType = "string"
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
	logFile                   *os.File
	imageName                 string
	imageId                   *string
	containerId               *string
	serviceDiscoveryDirectory string
	serviceDiscoveryFile      string
	metricsPort               int
}

func NewTestContext(ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient *client.Client, directory string, userApiKey string, userApiSecret string, logFile *os.File) *TestContext {
	return &TestContext{
		ctx:           ctx,
		clientFactory: clientFactory,
		dockerClient:  dockerClient,
		directory:     directory,
		userApiKey:    userApiKey,
		userApiSecret: userApiSecret,
		logFile:       logFile,
		imageName:     "fh-hand-in-test-image",
	}
}

func (tc *TestContext) InitializeTestSuite(_ *godog.TestSuiteContext) {

}

func (tc *TestContext) InitializeScenario(ctx *godog.ScenarioContext) {
	tempDir := os.TempDir()
	tc.serviceDiscoveryDirectory = path.Join(tempDir, "service-discovery")
	if _, err := os.Stat(tc.serviceDiscoveryDirectory); err == nil {
		_ = os.RemoveAll(tc.serviceDiscoveryDirectory)
	}
	if err := os.Mkdir(tc.serviceDiscoveryDirectory, os.ModePerm); err != nil {
		log.Printf("failed to create service discovery data directory %s (%v)", tc.serviceDiscoveryDirectory, err)
	}
	tc.serviceDiscoveryFile = path.Join(tc.serviceDiscoveryDirectory, "config.json")
	tc.metricsPort = rand.Intn(65534) + 1

	ctx.Step(`^I have applied the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
	ctx.Step(`^I apply the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
	ctx.Step(`^I have applied the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
	ctx.Step(`^I destroy using the Terraform code$`, tc.iHaveDestroyedUsingTheTerraformCode)
	ctx.Step(`^the tfstate file should be empty$`, tc.theTfstateFileShouldBeEmpty)
	ctx.Step(`^I set the instance pool to have (\d+) instances$`, tc.iSetTheInstancePoolToHaveInstances)
	ctx.Step(`^I kill all instances in the pool$`, tc.iKillAllInstancesInThePool)
	ctx.Step(`^I should receive the answer "([^"]*)" when querying the "([^"]*)" endpoint of the NLB$`, tc.iShouldReceiveTheAnswerWhenQueryingTheEndpointOfTheNLB)
	ctx.Step(`^I wait for (\d+) instances to be present$`, tc.iWaitForInstancesToBePresent)
	ctx.Step(`^I wait for no instances to be present$`, tc.iWaitForNoInstancesToBePresent)
	ctx.Step(`^one instance pool should exist$`, tc.oneInstancePoolShouldExist)
	ctx.Step(`^no instance pool should exist$`, tc.noInstancePoolShouldExist)
	ctx.Step(`^one NLB should exist$`, tc.oneNLBShouldExist)
	ctx.Step(`^no NLB should exist$`, tc.noNLBShouldExist)
	ctx.Step(`^the NLB should have one service$`, tc.theNLBShouldHaveOneService)
	ctx.Step(`^the service should listen to port (\d+)$`, tc.theServiceShouldListenToPort)
	ctx.Step(`^all backends should be healthy after (\d+) seconds$`, tc.allBackendsShouldBeHealthyAfterSeconds)
	ctx.Step(`^there must be a monitoring server$`, tc.thereMustBeAMonitoringServer)
	ctx.Step(`^Prometheus must be accessible on port (\d+) of the monitoring server within (\d+) seconds$`, tc.prometheusMustBeAccessible)
	ctx.Step(`^CPU metrics of all instances in the instance pool must be visible in Prometheus on port (\\d+) after (\\d+) seconds$`, tc.cPUMetricsOfAllInstancesInTheInstancePoolMustBeVisibleInPrometheusAfterMinutes)
	ctx.Step(`^I build the Dockerfile in the "([^"]*)" folder$`, tc.iBuildTheDockerfile)
	ctx.Step(`^I start a container from the image$`, tc.iStartAContainerFromTheImage)
	ctx.Step(`^the service discovery file must contain all instance pool IPs within (\d+) seconds$`, tc.theServiceDiscoveryFileMustContainAllInstancePoolIPsWithinSeconds)

	ctx.AfterScenario(func(sc *godog.Scenario, err error) {
		if err := os.RemoveAll(tc.serviceDiscoveryDirectory); err != nil {
			log.Printf("failed to remove service discovery directory (%v)", err)
		}
		if tc.containerId != nil {
			if err := tc.dockerClient.ContainerRemove(tc.ctx, *tc.containerId, types.ContainerRemoveOptions{
				RemoveVolumes: true,
				RemoveLinks:   true,
				Force:         true,
			}); err != nil {
				log.Printf("failed to remove container %s (%v)", *tc.containerId, err)
			}
		}
		if tc.imageId != nil {
			if _, err := tc.dockerClient.ImageRemove(tc.ctx, *tc.imageId, types.ImageRemoveOptions{
				Force:         true,
				PruneChildren: true,
			}); err != nil {
				log.Printf("failed to remove container image %s (%v)", *tc.imageId, err)
			}
		}
	})
}

func (tc *TestContext) iHaveInitializedTerraform() error {
	log.Printf("initializing Terraform...")
	err := executeTerraform(tc.ctx, tc.dockerClient, tc.directory, []string{
		"init",
		"-var", fmt.Sprintf("exoscale_key=%s", tc.userApiKey),
		"-var", fmt.Sprintf("exoscale_secret=%s", tc.userApiSecret),
	}, tc.logFile)
	if err != nil {
		return err
	}
	return nil
}

func (tc *TestContext) iHaveAppliedTheTerraformCode() error {
	log.Printf("applying Terraform code...")
	err := executeTerraform(tc.ctx, tc.dockerClient, tc.directory, []string{
		"apply",
		"-var", fmt.Sprintf("exoscale_key=%s", tc.userApiKey),
		"-var", fmt.Sprintf("exoscale_secret=%s", tc.userApiSecret),
	}, tc.logFile)
	if err != nil {
		return err
	}
	return nil
}

func (tc *TestContext) iHaveDestroyedUsingTheTerraformCode() error {
	log.Printf("destroying Terraform code...")
	return executeTerraform(
		tc.ctx, tc.dockerClient, tc.directory,
		[]string{
			"destroy",
			"-var", fmt.Sprintf("exoscale_key=%s", tc.userApiKey),
			"-var", fmt.Sprintf("exoscale_secret=%s", tc.userApiSecret),
		},
		tc.logFile,
	)
}

func (tc *TestContext) theTfstateFileShouldBeEmpty() error {
	log.Printf("checking if tfstate file is empty...")
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
	log.Printf("setting instance pool to %d instances...", instances)
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
	log.Printf("killing all instances in the instance pool...")
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
			log.Printf("killing instance %s...", vm.ID)
			go func() {
				_, err := exoscaleClient.RequestWithContext(tc.ctx, &egoscale.DestroyVirtualMachine{
					ID: vm.ID,
				})
				if err != nil {
					log.Printf("failed to remove instance %s (%v)", vm.ID, err)
				} else {
					log.Printf("removed instance %s", vm.ID)
				}
				wg.Done()
				errChannel <- err
			}()
		}
	}
	log.Printf("waiting for %d instances to be removed...", instances)
	wg.Wait()
	log.Printf("instance removal complete.")
	for i := 0; i < instances; i++ {
		err = <-errChannel
		if err != nil {
			return err
		}
	}
	return nil
}

func (tc *TestContext) iShouldReceiveTheAnswerWhenQueryingTheEndpointOfTheNLB(expectedAnswer string, endpoint string) error {
	log.Printf("querying NLB for response...")
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
			log.Printf("waiting for HTTP service to become available (%d)", retries*int(backoff/time.Second))
			time.Sleep(backoff)
			continue
		}
		body, err := ioutil.ReadAll(result.Body)
		if err != nil {
			return err
		}
		if string(body) != expectedAnswer {
			return fmt.Errorf("returned answer %s did not match expected answer %s", body, expectedAnswer)
		}
		return nil
	}
}

func (tc *TestContext) iWaitForInstancesToBePresent(instances int) error {
	log.Printf("waiting for %d instances to be present...", instances)
	backoff := 10 * time.Second
	retries := 0
	retryLimit := 30
	for {
		instancePool, _, err := tc.getInstancePool()
		if err != nil {
			log.Printf("failed to fetch instance pool (%v)", err)
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
			log.Printf("instances present")
			return nil
		}

		retries++
		if retries > retryLimit {
			return fmt.Errorf("invalid number of running instances (%d) for instance pool, expected %d", runningVMs, instances)
		}
		log.Printf("waiting for %d instances to be present, currently %d (%d)", instances, runningVMs, retries*int(backoff/time.Second))
		time.Sleep(backoff)
	}
}

func (tc *TestContext) iWaitForNoInstancesToBePresent() error {
	log.Printf("waiting for no instances to be present...")
	return tc.iWaitForInstancesToBePresent(0)
}

func (tc *TestContext) oneInstancePoolShouldExist() error {
	log.Printf("checking if exactly one instance pool exists...")
	_, _, err := tc.getInstancePool()
	return err
}

func (tc *TestContext) noInstancePoolShouldExist() error {
	log.Printf("checking if exactly no instance pools exist...")
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
	log.Printf("checking if exactly one NLB exists...")
	_, _, err := tc.getNLB()
	return err
}

func (tc *TestContext) noNLBShouldExist() error {
	log.Printf("Checking if no NLBs exist...")
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
	log.Printf("checking if the service listens to port %d...", port)
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
	log.Printf("waiting %d seconds for all backends to be healthy...", seconds)
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
			log.Printf("%d out of %d backends for the service are unhealthy (%d)", unhealthyBackends, len(service.HealthcheckStatus), tries*int(backoff/time.Second))
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
	log.Printf("checking for one monitoring server to be present...")
	_, err := tc.getMonitoringServerIp()
	return err
}

func (tc *TestContext) prometheusMustBeAccessible(port int, seconds int) error {
	log.Printf("checking if Prometheus is accessible on port %d after %d seconds...", port, seconds)
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
			log.Printf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		if response.StatusCode != 200 {
			log.Printf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Printf("failed to read response from %s (%v)", monitoringUrl, err)
			continue
		}
		data := &prometheusResponse{}
		err = json.Unmarshal(body, data)
		if err != nil {
			log.Printf("failed to decode JSON from Prometheus (%v)", err)
			continue
		}
		if data.Status != prometheusStatusSuccess {
			log.Printf("error from Prometheus during query (%v)", data.Error)
			continue
		}
		if data.Data.ResultType != prometheusResultTypeVector {
			log.Printf("unexpected response type from Prometheus (%v)", data.Data.ResultType)
			continue
		}
		log.Printf("Prometheus is up")
		return nil
	}
}

func (tc *TestContext) cPUMetricsOfAllInstancesInTheInstancePoolMustBeVisibleInPrometheusAfterMinutes(port int, seconds int) error {
	log.Printf("checking if all instances in the pool have their CPU metrics visible in Prometheus on port %d after %d seconds...", port, seconds)
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
			log.Printf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		if response.StatusCode != 200 {
			log.Printf("failed to query %s (%v)", monitoringUrl, err)
			continue
		}
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Printf("failed to read response from %s (%v)", monitoringUrl, err)
			continue
		}
		data := &prometheusResponse{}
		err = json.Unmarshal(body, data)
		if err != nil {
			log.Printf("failed to decode JSON from Prometheus (%v)", err)
			continue
		}
		if data.Status != prometheusStatusSuccess {
			log.Printf("error from Prometheus during query (%v)", data.Error)
			continue
		}
		if data.Data.ResultType != prometheusResultTypeVector {
			log.Printf("unexpected response type from Prometheus (%v)", data.Data.ResultType)
			continue
		}
		instancePool, _, err := tc.getInstancePool()
		if err != nil {
			log.Printf("failed to get instance pool (%v)", data.Data.ResultType)
			continue
		}
		metricIps := map[string]bool{}
		for _, record := range data.Data.Result {
			parts := strings.SplitN(record.Metric.Instance, ":", 2)
			metricIps[parts[0]] = true
		}
		allIpsFound := true
		for _, vm := range instancePool.VirtualMachines {
			for _, nic := range vm.Nic {
				if nic.IsDefault {
					if _, ok := metricIps[nic.IPAddress.String()]; !ok {
						allIpsFound = false
					}
				}
			}
		}
		if !allIpsFound {
			log.Printf("not all IP's are in Prometheus")
			continue
		}

		return nil
	}
}

func (tc *TestContext) iBuildTheDockerfile(folder string) error {
	log.Printf("building the Dockerfile in the %s directory...", folder)
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
	defer writer.Close()
	defer reader.Close()

	go func() {
		if err := tarDirectory(directory, writer); err != nil {
			log.Printf("failed to build tar for Docker build (%v)", err)
		}
		if err := writer.CloseWithError(io.EOF); err != nil {
			log.Printf("failed to close writer (%v)", err)
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

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		decoder := json.NewDecoder(strings.NewReader(line))
		data := make(map[string]interface{})
		if err := decoder.Decode(&data); err != nil {
			return fmt.Errorf("failed to decode JSON line: %s (%v)", line, err)
		}
		if val, ok := data["stream"]; ok {
			log.Printf("Docker build: %s", val)
		} else if val, ok := data["aux"]; ok {
			aux := val.(map[string]interface{})
			if val2, ok2 := aux["ID"]; ok2 {
				id := val2.(string)
				tc.imageId = &id
			} else {
				return fmt.Errorf("invalid response type from Docker daemon: %s", line)
			}
		} else if val, ok := data["status"]; ok {
			status := val.(string)
			id := ""
			if data["id"] != nil {
				id = data["id"].(string)
			}
			log.Printf("Docker build: %s:%s", status, id)
		} else {
			return fmt.Errorf("invalid response type from Docker daemon: %s", line)
		}
	}

	log.Printf("image build completed successfully")
	return nil
}

func (tc *TestContext) iStartAContainerFromTheImage() error {
	if tc.imageId == nil {
		return fmt.Errorf("container image has not been built")
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
			},
			Image:        *tc.imageId,
			AttachStdout: true,
			AttachStderr: true,
		},
		&container.HostConfig{
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: tc.serviceDiscoveryDirectory,
					Target: "/srv/service-discovery",
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
	tc.containerId = &id

	attachOptions := types.ContainerAttachOptions{
		Logs:   true,
		Stdin:  false,
		Stderr: true,
		Stdout: true,
		Stream: true,
	}
	attachResult, err := tc.dockerClient.ContainerAttach(tc.ctx, *tc.containerId, attachOptions)
	if err != nil {
		return fmt.Errorf("failed to attach to container (%v)", err)
	}

	startOptions := types.ContainerStartOptions{}
	err = tc.dockerClient.ContainerStart(tc.ctx, *tc.containerId, startOptions)
	if err != nil {
		return fmt.Errorf("failed to start container (%v)", err)
	}

	go func() {
		_, err = stdcopy.StdCopy(tc.logFile, tc.logFile, attachResult.Reader)
		if err != nil {
			log.Printf("failed to stream logs from Docker (%v)", err)
		}
		inspect, err := tc.dockerClient.ContainerInspect(tc.ctx, *tc.containerId)
		if err != nil {
			log.Printf("failed to inspect container after exit (%v)", err)
			return
		}
		if inspect.State.ExitCode != 0 {
			log.Printf("container exited with non-zero status code (%d)", inspect.State.ExitCode)
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
	startTime := time.Now()
	maxEndTime := startTime.Add(time.Duration(seconds) * time.Second)
	for {
		log.Printf("checking if all IPs in the instance pool are present in the service discovery file...")
		if time.Now().After(maxEndTime) {
			log.Printf("timeout while waiting for service discovery")
			return fmt.Errorf("timeout while waiting for service discovery")
		}
		_, err := os.Stat(tc.serviceDiscoveryFile)
		if err != nil {
			log.Printf("service discovery file does not exist yet...")
			time.Sleep(10 * time.Second)
			continue
		}
		fh, err := os.Open(tc.serviceDiscoveryFile)
		if err != nil {
			log.Printf("failed to open service discovey file (%v)", err)
			time.Sleep(10 * time.Second)
			continue
		}
		decoder := json.NewDecoder(fh)
		decoded := &ServiceDiscoveryFile{}
		if err := decoder.Decode(decoded); err != nil {
			log.Printf("failed to decode service discovery file (%v)", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var decodedTargets = make(map[string]bool)
		for _, record := range *decoded {
			for _, entry := range record.Targets {
				decodedTargets[entry] = true
			}
		}

		instancePoolIps, err := tc.getInstancePoolIps()
		if err != nil {
			log.Printf("failed to fetch IPs from instance pool (%v)", err)
		}

		if len(decodedTargets) != len(instancePoolIps) {
			log.Printf("Number targets in the service discovery file (%d) does not match IPs in the instance pool (%d)", len(decodedTargets), len(instancePoolIps))
			time.Sleep(10 * time.Second)
			continue
		}

		allIpsPresent := true
		for _, ip := range instancePoolIps {
			target := fmt.Sprintf("%s:%d", ip.String(), tc.metricsPort)
			if _, ok := decodedTargets[target]; !ok {
				log.Printf("expected target %s is not in the service discovery file", target)
				allIpsPresent = false
			}
		}
		if !allIpsPresent {
			time.Sleep(10 * time.Second)
			continue
		}
		log.Printf("all IPs are present in the service discovery file")
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
