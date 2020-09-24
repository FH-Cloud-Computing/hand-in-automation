package main

import (
	"context"
	"fmt"
	"github.com/cucumber/godog"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/janoszen/exoscale-account-wiper/plugin"
	"time"
)

type TestContext struct {
	ctx context.Context
	clientFactory *plugin.ClientFactory
	dockerClient  *client.Client
	directory string
	userApiKey string
	userApiSecret string
}

func NewTestContext(ctx context.Context, clientFactory *plugin.ClientFactory, dockerClient *client.Client, directory string, userApiKey string, userApiSecret string) *TestContext {
	return &TestContext{
		ctx: ctx,
		clientFactory: clientFactory,
		dockerClient: dockerClient,
		directory: directory,
		userApiKey: userApiKey,
		userApiSecret: userApiSecret,
	}
}

func (tc *TestContext) InitializeTestSuite(ctx *godog.TestSuiteContext) {

}

func (tc *TestContext) InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Step(`^I have applied the Terraform code$`, tc.iHaveAppliedTheTerraformCode)
	ctx.Step(`^I resize the instance pool to two$`, tc.iResizeTheInstancePoolToTwo)
	ctx.Step(`^I resize the instance pool to zero$`, tc.iResizeTheInstancePoolToZero)
	ctx.Step(`^I should receive the answer "([^"]*)" when querying the "([^"]*)" endpoint of the NLB$`, tc.iShouldReceiveTheAnswerWhenQueryingTheEndpointOfTheNLB)
	ctx.Step(`^I wait for (\d+) instances to be present$`, tc.iWaitForInstancesToBePresent)
	ctx.Step(`^I wait for no instances to be present$`, tc.iWaitForNoInstancesToBePresent)
	ctx.Step(`^one instance pool should exist$`, tc.oneInstancePoolShouldExist)
	ctx.Step(`^one NLB should exist$`, tc.oneNLBShouldExist)
	ctx.Step(`^the NLB should have one service$`, tc.theNLBShouldHaveOneService)
	ctx.Step(`^the service should listen to port (\d+)$`, tc.theServiceShouldListenToPort)
	ctx.Step("^all backends should be healthy after (\\d+) seconds$", tc.allBackendsShouldBeHealthyAfterSeconds)
}

func (tc *TestContext) iHaveAppliedTheTerraformCode() error {
	err := executeTerraform(
		tc.ctx,
		tc.dockerClient,
		tc.directory,
		[]string{
			"apply",
			"-var", fmt.Sprintf("exoscale_key=%s", tc.userApiKey),
			"-var", fmt.Sprintf("exoscale_secret=%s", tc.userApiSecret),
		},
	)
	if err != nil {
		return err
	}
	return nil
}

func (tc *TestContext) iResizeTheInstancePoolToTwo() error {
	return godog.ErrPending
}

func (tc *TestContext) iResizeTheInstancePoolToZero() error {
	return godog.ErrPending
}

func (tc *TestContext) iShouldReceiveTheAnswerWhenQueryingTheEndpointOfTheNLB(arg1, arg2 string) error {
	return godog.ErrPending
}

func (tc *TestContext) iWaitForInstancesToBePresent(arg1 int) error {
	return godog.ErrPending
}

func (tc *TestContext) iWaitForNoInstancesToBePresent() error {
	return godog.ErrPending
}

func (tc *TestContext) oneInstancePoolShouldExist() error {
	_, _, err := tc.getInstancePool()
	return err
}

func (tc *TestContext) getInstancePool() (*egoscale.InstancePool, *egoscale.Zone, error) {
	exoscaleClient := tc.clientFactory.GetExoscaleClient()
	resp, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListZones{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list zones (%v)", err)
	}
	instancePoolCount := 0
	var instancePoolNames []string
	var instancePool *egoscale.InstancePool
	var zone *egoscale.Zone
	for _, z := range resp.(*egoscale.ListZonesResponse).Zone {
		resp, err := exoscaleClient.RequestWithContext(tc.ctx, egoscale.ListInstancePools{ZoneID: z.ID})
		if err != nil {
			return nil, nil, err
		}

		for _, i := range resp.(*egoscale.ListInstancePoolsResponse).InstancePools {
			instancePoolCount++
			instancePoolNames = append(instancePoolNames, i.Name)
			instancePool = &i
			zone = &z
		}
	}
	if instancePoolCount != 1 {
		return nil, nil, fmt.Errorf("invalid number of Instance Pools: %d %v", instancePoolCount, instancePoolNames)
	}
	return instancePool, zone, nil
}

func (tc *TestContext) oneNLBShouldExist() error {
	_, _, err := tc.getNLB()
	return err
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
			if status.Status != "healthy" {
				unhealthyBackends++
			}
		}
		if unhealthyBackends != 0 {
			tries++
			if tries > seconds/int(backoff/time.Second) {
				return fmt.Errorf("%d out of %d backends for the service are unhealthy", unhealthyBackends, len(service.HealthcheckStatus))
			}
			time.Sleep(backoff)
		} else {
			return nil
		}
	}
}