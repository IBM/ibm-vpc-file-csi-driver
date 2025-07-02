/**
 * Copyright 2022 IBM Corp.
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

package secret_provider

import (
	"encoding/base64"
	"os"
	"strings"

	localutils "github.com/IBM/secret-common-lib/pkg/utils"
	auth "github.com/IBM/secret-utils-lib/pkg/authenticator"
	"github.com/IBM/secret-utils-lib/pkg/config"
	"github.com/IBM/secret-utils-lib/pkg/k8s_utils"
	"github.com/IBM/secret-utils-lib/pkg/utils"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

// UnmanagedSecretProvider ...
type UnmanagedSecretProvider struct {
	authenticator            auth.Authenticator
	logger                   *zap.Logger
	k8sClient                k8s_utils.KubernetesClient
	authType                 string
	tokenExchangeURL         string
	region                   string
	riaasEndpoint            string
	privateRIAASEndpoint     string
	containerAPIRoute        string
	privateContainerAPIRoute string
	resourceGroupID          string
	providedTokenExchangeURL bool
}

// newUnmanagedSecretProvider ...
func newUnmanagedSecretProvider(k8sClient *k8s_utils.KubernetesClient, logger *zap.Logger, optionalArgs ...map[string]string) (*UnmanagedSecretProvider, error) {
	// Validate the argument k8s client
	validate := validator.New()
	err := validate.Struct(k8sClient)
	if err != nil {
		logger.Error("Provided k8s client is invalid", zap.Error(err))
		return nil, utils.Error{Description: "Error initialising k8s client", BackendError: err.Error()}
	}

	return InitUnmanagedSecretProvider(logger, *k8sClient, optionalArgs...)
}

// initUnmanagedSecretProvider ...
func InitUnmanagedSecretProvider(logger *zap.Logger, kc k8s_utils.KubernetesClient, optionalArgs ...map[string]string) (*UnmanagedSecretProvider, error) {
	authenticator, authType, err := auth.NewAuthenticator(logger, kc, optionalArgs...)
	if err != nil {
		logger.Error("Error initializing unmanaged secret provider", zap.Error(err))
		return nil, err
	}

	if authenticator.IsSecretEncrypted() {
		logger.Error("Secret is encrypted, decryption is only supported by sidecar container")
		return nil, utils.Error{Description: localutils.ErrDecryptionNotSupported}
	}

	// Checking if the secret(api key) needs to be decoded
	if authType == utils.DEFAULT && os.Getenv("IS_SATELLITE") == "True" {
		logger.Info("Decoding apiKey since it's a satellite cluster")
		decodedSecret, err := base64.StdEncoding.DecodeString(authenticator.GetSecret())
		if err != nil {
			logger.Error("Error decoding the secret", zap.Error(err))
			return nil, err
		}
		// In the decoded secret, newline could be present, trimming the same to extract a valid api key.
		authenticator.SetSecret(strings.TrimSuffix(string(decodedSecret), "\n"))
	}

	usp := new(UnmanagedSecretProvider)
	usp.authenticator = authenticator
	usp.logger = logger
	usp.authType = authType
	usp.k8sClient = kc

	err = usp.initEndpointsUsingCloudConf()
	// If token exchange URL is also initialised using cloud conf, return.
	if usp.tokenExchangeURL != "" {
		logger.Info("Initialized unmanaged secret provider")
		return usp, nil
	}

	cc, _ := config.GetClusterInfo(kc, logger)

	var providerName string
	if len(optionalArgs) == 1 {
		providerName, _ = optionalArgs[0][ProviderType]
	}
	if providerName == "" {
		providerName = utils.VPC
	}
	err = usp.initEndpointsUsingStorageSecretStore(cc, providerName)
	if usp.tokenExchangeURL != "" {
		logger.Info("Initialized unmanaged secret provider")
		return usp, nil
	}

	usp.tokenExchangeURL, usp.providedTokenExchangeURL = config.FrameTokenExchangeURLFromClusterInfo(cc, logger)
	usp.authenticator.SetURL(usp.tokenExchangeURL, usp.providedTokenExchangeURL)
	logger.Info("Framed token exhange URL from cluster info")
	logger.Info("Initialized unmanaged secret provider")
	return usp, nil
}

// GetDefaultIAMToken ...
func (usp *UnmanagedSecretProvider) GetDefaultIAMToken(isFreshTokenRequired bool, reasonForCall ...string) (string, uint64, error) {
	usp.logger.Info("In GetDefaultIAMToken()")
	return usp.authenticator.GetToken(true)
}

// GetIAMToken ...
func (usp *UnmanagedSecretProvider) GetIAMToken(secret string, isFreshTokenRequired bool, reasonForCall ...string) (string, uint64, error) {
	usp.logger.Info("In GetIAMToken()")
	var authenticator auth.Authenticator
	switch usp.authType {
	case utils.IAM, utils.DEFAULT:
		authenticator = auth.NewIamAuthenticator(secret, usp.logger)
	case utils.PODIDENTITY:
		authenticator = auth.NewComputeIdentityAuthenticator(secret, usp.logger)
	}

	authenticator.SetURL(usp.tokenExchangeURL, usp.providedTokenExchangeURL)
	token, tokenlifetime, err := authenticator.GetToken(true)
	if err != nil {
		usp.logger.Error("Error fetching IAM token", zap.Error(err))
		return token, tokenlifetime, err
	}
	return token, tokenlifetime, nil
}

// GetRIAASEndpoint ...
func (usp *UnmanagedSecretProvider) GetRIAASEndpoint(readConfig bool) (string, error) {
	usp.logger.Info("In GetRIAASEndpoint()")
	if !readConfig {
		usp.logger.Info("Returning RIAAS endpoint", zap.String("Endpoint", usp.riaasEndpoint))
		return usp.riaasEndpoint, nil
	}

	endpoint, err := getEndpoint(localutils.RIAAS, usp.riaasEndpoint, usp.k8sClient, usp.logger)
	if err != nil {
		return "", err
	}

	usp.riaasEndpoint = endpoint
	return endpoint, nil
}

// GetPrivateRIAASEndpoint ...
func (usp *UnmanagedSecretProvider) GetPrivateRIAASEndpoint(readConfig bool) (string, error) {
	usp.logger.Info("In GetPrivateRIAASEndpoint()")
	if !readConfig {
		usp.logger.Info("Returning private RIAAS endpoint", zap.String("Endpoint", usp.privateRIAASEndpoint))
		return usp.privateRIAASEndpoint, nil
	}

	endpoint, err := getEndpoint(localutils.PrivateRIAAS, usp.privateRIAASEndpoint, usp.k8sClient, usp.logger)
	if err != nil {
		return "", err
	}

	usp.privateRIAASEndpoint = endpoint
	return endpoint, nil
}

// GetContainerAPIRoute ...
func (usp *UnmanagedSecretProvider) GetContainerAPIRoute(readConfig bool) (string, error) {
	usp.logger.Info("In GetContainerAPIRoute()")
	if !readConfig {
		usp.logger.Info("Returning container api route", zap.String("Endpoint", usp.containerAPIRoute))
		return usp.containerAPIRoute, nil
	}

	endpoint, err := getEndpoint(localutils.ContainerAPIRoute, usp.containerAPIRoute, usp.k8sClient, usp.logger)
	if err != nil {
		return "", err
	}

	usp.containerAPIRoute = endpoint
	return endpoint, nil
}

// GetPrivateContainerAPIRoute ...
func (usp *UnmanagedSecretProvider) GetPrivateContainerAPIRoute(readConfig bool) (string, error) {
	usp.logger.Info("In GetPrivateContainerAPIRoute()")
	if !readConfig {
		usp.logger.Info("Returning private container api route", zap.String("Endpoint", usp.privateContainerAPIRoute))
		return usp.privateContainerAPIRoute, nil
	}

	endpoint, err := getEndpoint(localutils.PrivateContainerAPIRoute, usp.privateContainerAPIRoute, usp.k8sClient, usp.logger)
	if err != nil {
		return "", err
	}

	usp.privateContainerAPIRoute = endpoint
	return endpoint, nil
}

// GetResourceGroupID ...
func (usp *UnmanagedSecretProvider) GetResourceGroupID() string {
	return usp.resourceGroupID
}

// initEndpointsUsingCloudConf ...
func (usp *UnmanagedSecretProvider) initEndpointsUsingCloudConf() error {
	cloudConf, err := config.GetCloudConf(usp.logger, usp.k8sClient)
	if err != nil {
		return err
	}

	usp.region = cloudConf.Region
	usp.containerAPIRoute = cloudConf.ContainerAPIRoute
	usp.privateContainerAPIRoute = cloudConf.PrivateContainerAPIRoute
	usp.riaasEndpoint = cloudConf.RiaasEndpoint
	usp.privateRIAASEndpoint = cloudConf.PrivateRIAASEndpoint
	usp.resourceGroupID = cloudConf.ResourceGroupID
	usp.logger.Info("Initialised endpoints using cloud-conf")
	if cloudConf.TokenExchangeURL != "" {
		usp.logger.Info("Using the token exchange URL provided in cloud-conf")
		usp.tokenExchangeURL = cloudConf.TokenExchangeURL
		usp.authenticator.SetURL(usp.tokenExchangeURL, true)
		usp.providedTokenExchangeURL = true
		return nil
	}

	usp.logger.Info("Token exchange URL not provided in cloud-conf")
	return nil
}

// initEndpointsUsingStorageSecretStore ...
func (usp *UnmanagedSecretProvider) initEndpointsUsingStorageSecretStore(cc config.ClusterConfig, providerType string) error {
	data, err := k8s_utils.GetSecretData(usp.k8sClient, utils.STORAGE_SECRET_STORE_SECRET, utils.SECRET_STORE_FILE)
	var conf *config.Config
	if err != nil {
		usp.logger.Warn("Error fetching storage-secret-store data", zap.Error(err))
		return err
	}

	conf, err = config.ParseConfig(usp.logger, data)
	if err != nil {
		usp.logger.Warn("Error parsing storage-secret-store data", zap.Error(err))
		return err
	}

	usp.containerAPIRoute = conf.Bluemix.APIEndpointURL
	usp.privateContainerAPIRoute = conf.Bluemix.PrivateAPIRoute
	usp.riaasEndpoint = conf.VPC.G2EndpointURL
	usp.privateRIAASEndpoint = conf.VPC.G2EndpointPrivateURL
	usp.resourceGroupID = conf.VPC.G2ResourceGroupID
	usp.logger.Info("Fetched endpoints from storage-secret-store")

	tokenExchangeURL, providedTokenExchangeURL, err := config.GetTokenExchangeURLfromStorageSecretStore(cc, *conf, providerType)
	if err == nil {
		usp.logger.Info("Framed token exchange URL using storage-secret-store")
		usp.tokenExchangeURL = tokenExchangeURL
		usp.providedTokenExchangeURL = providedTokenExchangeURL
		usp.authenticator.SetURL(tokenExchangeURL, providedTokenExchangeURL)
		return nil
	}

	usp.logger.Info("Unable to fetch token exchange URL from storage-secret-store")
	return nil
}
