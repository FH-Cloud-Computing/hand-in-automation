package main

import (
	"context"
	"github.com/janoszen/exoscale-account-wiper/factory"
	"github.com/janoszen/exoscale-account-wiper/plugin"
	"log"
)

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

