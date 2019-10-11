package main

import (
	"fmt"
	"net/http"

	"github.com/Azure/go-autorest/autorest/adal"
)

// MSDC settings
type MSDC struct {
	tenant      string
	application string

	client      *http.Client
	oauthConfig *adal.OAuthConfig
}

// NewMSDC configuration
func NewMSDC(tenant, application string) (*MSDC, error) {
	oauthConfig, err := adal.NewOAuthConfig("https://login.microsoftonline.com/", tenant)
	if err != nil {
		return nil, err
	}

	return &MSDC{
		tenant:      tenant,
		application: application,
		client:      &http.Client{},
		oauthConfig: oauthConfig,
	}, nil
}

// Callback checks if a token is still valid
func (msdc *MSDC) Callback(token adal.Token) error {
	if token.IsExpired() {
		return fmt.Errorf("Token has expired")
	}
	return nil
}

// Get acquires the device code
func (msdc *MSDC) Get(resource string) (*adal.DeviceCode, error) {
	// Acquire the device code
	return adal.InitiateDeviceAuth(
		msdc.client,
		*msdc.oauthConfig,
		msdc.application,
		resource)
}

// WaitForUserCompletion is waiting until the user is authenticated
func (msdc *MSDC) WaitForUserCompletion(deviceCode *adal.DeviceCode) (*adal.Token, error) {
	return adal.WaitForUserCompletion(msdc.client, deviceCode)
}

// CheckResource if user has access to this resource
func (msdc *MSDC) CheckResource(token adal.Token, resource string) (*adal.ServicePrincipalToken, error) {
	return adal.NewServicePrincipalTokenFromManualToken(
		*msdc.oauthConfig,
		msdc.application,
		resource,
		token,
		msdc.Callback)
}
