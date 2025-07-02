/**
 * Copyright 2021 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package provider ...
package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	vpcauth "github.com/IBM/ibmcloud-volume-file-vpc/common/auth"
	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/riaas"
	vpcconfig "github.com/IBM/ibmcloud-volume-file-vpc/file/vpcconfig"
	"github.com/IBM/ibmcloud-volume-interface/config"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"github.com/IBM/ibmcloud-volume-interface/provider/iam"
	"github.com/IBM/ibmcloud-volume-interface/provider/local"
	"github.com/IBM/secret-utils-lib/pkg/k8s_utils"
	"go.uber.org/zap"
)

const (
	// VPCNextGen ...
	VPCNextGen = "g2"
	// PrivatePrefix ...
	PrivatePrefix = "private-"
	// BasePrivateURL ...
	BasePrivateURL = "https://" + PrivatePrefix
	// HTTPSLength ...
	HTTPSLength = 8
	// NEXTGenProvider ...
	NEXTGenProvider = 2
)

// VPCFileProvider implements provider.Provider
type VPCFileProvider struct {
	timeout        time.Duration
	Config         *vpcconfig.VPCFileConfig
	tokenGenerator *tokenGenerator
	ContextCF      local.ContextCredentialsFactory

	ClientProvider riaas.RegionalAPIClientProvider
	httpClient     *http.Client
	APIConfig      riaas.Config
}

var _ local.Provider = &VPCFileProvider{}

// NewProvider initialises an instance of an IaaS provider.
func NewProvider(conf *vpcconfig.VPCFileConfig, k8sClient *k8s_utils.KubernetesClient, logger *zap.Logger) (local.Provider, error) {
	logger.Info("Entering NewProvider")

	if conf.VPCConfig == nil {
		return nil, errors.New("incomplete config for VPCFileProvider")
	}

	if conf.VPCConfig.G2EndpointPrivateURL != "" {
		conf.VPCConfig.G2EndpointURL = conf.VPCConfig.G2EndpointPrivateURL
	}

	//Set API Generation As 2
	conf.VPCConfig.G2VPCAPIGeneration = NEXTGenProvider

	//Set the APIVersion Date, it can be different in GC and NG
	if conf.VPCConfig.G2APIVersion != "" {
		conf.VPCConfig.APIVersion = conf.VPCConfig.G2APIVersion
	}

	//set provider-type
	conf.VPCConfig.VPCBlockProviderType = VPCNextGen

	//Mark this as enabled/active
	conf.VPCConfig.VPCTypeEnabled = VPCNextGen

	contextCF, err := vpcauth.NewVPCContextCredentialsFactory(conf, k8sClient)
	if err != nil {
		return nil, err
	}
	timeoutString := conf.VPCConfig.VPCTimeout
	if timeoutString == "" || timeoutString == "0s" {
		logger.Info("Using VPC default timeout")
		timeoutString = "120s"
	}
	timeout, err := time.ParseDuration(timeoutString)
	if err != nil {
		return nil, err
	}

	httpClient, err := config.GeneralCAHttpClientWithTimeout(timeout)
	if err != nil {
		logger.Error("Failed to prepare HTTP client", util.ZapError(err))
		return nil, err
	}

	// SetRetryParameters sets the retry logic parameters
	SetRetryParameters(conf.VPCConfig.MaxRetryAttempt, conf.VPCConfig.MaxRetryGap)
	provider := &VPCFileProvider{
		timeout:        timeout,
		Config:         conf,
		tokenGenerator: &tokenGenerator{config: conf.VPCConfig},
		ContextCF:      contextCF,
		httpClient:     httpClient,
		APIConfig: riaas.Config{
			BaseURL:       conf.VPCConfig.G2EndpointURL,
			HTTPClient:    httpClient,
			APIVersion:    conf.VPCConfig.G2APIVersion,
			APIGeneration: conf.VPCConfig.G2VPCAPIGeneration,
			ResourceGroup: conf.VPCConfig.G2ResourceGroupID,
		},
	}
	userError.MessagesEn = userError.InitMessages()
	return provider, nil
}

// ContextCredentialsFactory ...
func (vpcp *VPCFileProvider) ContextCredentialsFactory(zone *string) (local.ContextCredentialsFactory, error) {
	//  Datacenter name not required by VPC provider implementation
	return vpcp.ContextCF, nil
}

// OpenSession opens a session on the provider
func (vpcp *VPCFileProvider) OpenSession(ctx context.Context, contextCredentials provider.ContextCredentials, ctxLogger *zap.Logger) (provider.Session, error) {
	ctxLogger.Info("Entering OpenSession")
	defer metrics.UpdateDurationFromStart(ctxLogger, "OpenSession", time.Now())
	defer func() {
		ctxLogger.Debug("Exiting OpenSession")
	}()

	// validate that we have what we need - i.e. valid credentials
	if contextCredentials.Credential == "" {
		return nil, util.NewError("Error Insufficient Authentication", "No authentication credential provided")
	}

	if vpcp.Config.ServerConfig.DebugTrace {
		vpcp.APIConfig.DebugWriter = os.Stdout
	}

	if vpcp.ClientProvider == nil {
		vpcp.ClientProvider = riaas.DefaultRegionalAPIClientProvider{}
	}
	ctxLogger.Debug("", zap.Reflect("apiConfig.BaseURL", vpcp.APIConfig.BaseURL))

	if ctx != nil && ctx.Value(provider.RequestID) != nil {
		// set ContextID only of speicifed in the context
		vpcp.APIConfig.ContextID = fmt.Sprintf("%v", ctx.Value(provider.RequestID))
	}
	client, err := vpcp.ClientProvider.New(vpcp.APIConfig)
	if err != nil {
		return nil, err
	}

	// Create a token for all other API calls
	token, err := getAccessToken(contextCredentials, ctxLogger)
	if err != nil {
		return nil, err
	}
	ctxLogger.Debug("", zap.Reflect("Token", token.Token))

	err = client.Login(token.Token)
	if err != nil {
		return nil, err
	}

	// Update retry logic default values
	if vpcp.Config.VPCConfig.MaxRetryAttempt > 0 {
		ctxLogger.Debug("", zap.Reflect("MaxRetryAttempt", vpcp.Config.VPCConfig.MaxRetryAttempt))
		maxRetryAttempt = vpcp.Config.VPCConfig.MaxRetryAttempt
	}
	if vpcp.Config.VPCConfig.MaxRetryGap > 0 {
		ctxLogger.Debug("", zap.Reflect("MaxRetryGap", vpcp.Config.VPCConfig.MaxRetryGap))
		maxRetryGap = vpcp.Config.VPCConfig.MaxRetryGap
	}

	vpcSession := &VPCSession{
		VPCAccountID:       contextCredentials.IAMAccountID,
		Config:             vpcp.Config,
		ContextCredentials: contextCredentials,
		VolumeType:         "vpc-share",
		Provider:           VPC,
		Apiclient:          client,
		Logger:             ctxLogger,
		APIRetry:           NewFlexyRetryDefault(),
	}

	return vpcSession, nil
}

// getAccessToken ...
func getAccessToken(creds provider.ContextCredentials, logger *zap.Logger) (token *iam.AccessToken, err error) {
	switch creds.AuthType {
	case provider.IAMAccessToken:
		token = &iam.AccessToken{Token: creds.Credential}
	default:
		err = errors.New("unknown AuthType")
	}
	return
}

// getPrivateEndpoint ...
func getPrivateEndpoint(logger *zap.Logger, publicEndPoint string) string {
	logger.Info("In getPrivateEndpoint, RIaaS public endpoint", zap.Reflect("URL", publicEndPoint))
	if !strings.Contains(publicEndPoint, PrivatePrefix) {
		if len(publicEndPoint) > HTTPSLength {
			return BasePrivateURL + publicEndPoint[HTTPSLength:]
		}
	} else {
		return publicEndPoint
	}
	return ""
}
