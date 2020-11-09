package main

import (
	"context"

	log2 "github.com/containerssh/log"
	"github.com/janoszen/exoscale-account-wiper/factory"
	"github.com/janoszen/exoscale-account-wiper/plugin"
)

func wipeAccount(ctx context.Context, clientFactory *plugin.ClientFactory, logger log2.Logger) error {
	logger.Infof("wiping Exoscale account...")
	wiper := factory.CreateRegistry()
	err := wiper.SetConfiguration(map[string]string{"iam-exclude-self": "1"}, false)
	if err != nil {
		return err
	}
	err = wiper.Run(clientFactory, ctx)
	if err != nil {
		logger.Warningf("failed to wipe Exoscale account (%v).", err)
		return err
	}
	logger.Debugf("wiped Exoscale account.")
	return nil
}
