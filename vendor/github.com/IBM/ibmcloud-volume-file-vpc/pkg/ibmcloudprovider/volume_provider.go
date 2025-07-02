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

// Package ibmcloudprovider ...
package ibmcloudprovider

import (
	"fmt"
	"os"
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/registry"
	provider_util "github.com/IBM/ibmcloud-volume-file-vpc/file/utils"
	vpcconfig "github.com/IBM/ibmcloud-volume-file-vpc/file/vpcconfig"
	"github.com/IBM/ibmcloud-volume-interface/config"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"github.com/IBM/ibmcloud-volume-interface/provider/local"
	utilsConfig "github.com/IBM/secret-utils-lib/pkg/config"
	"github.com/IBM/secret-utils-lib/pkg/k8s_utils"
	"go.uber.org/zap"
	"golang.org/x/net/context"
)

// IBMCloudStorageProvider Provider
type IBMCloudStorageProvider struct {
	ProviderName   string
	ProviderConfig *config.Config
	Registry       registry.Providers
	ClusterID      string
}

var _ CloudProviderInterface = &IBMCloudStorageProvider{}

// NewIBMCloudStorageProvider ...
func NewIBMCloudStorageProvider(clusterVolumeLabel string, k8sClient *k8s_utils.KubernetesClient, logger *zap.Logger) (*IBMCloudStorageProvider, error) {
	logger.Info("NewIBMCloudStorageProvider-Reading provider configuration...")
	// Load config file
	conf, err := config.ReadConfig(*k8sClient, logger)
	if err != nil {
		logger.Error("Error loading configuration")
		return nil, err
	}
	// Get only VPC_API_VERSION, in "2019-07-02T00:00:00.000Z" case vpc need only 2019-07-02"
	dateTime, err := time.Parse(time.RFC3339, conf.VPC.APIVersion)
	if err == nil {
		conf.VPC.APIVersion = fmt.Sprintf("%d-%02d-%02d", dateTime.Year(), dateTime.Month(), dateTime.Day())
	} else {
		logger.Warn("Failed to parse VPC_API_VERSION, setting default value")
		conf.VPC.APIVersion = "2023-07-11" // setting default values
	}

	var clusterInfo utilsConfig.ClusterConfig
	if conf.IKS != nil && conf.IKS.Enabled || os.Getenv("IKS_ENABLED") == "True" {
		logger.Info("Fetching clusterInfo")
		clusterInfo, err = utilsConfig.GetClusterInfo(*k8sClient, logger)
		if err != nil {
			logger.Error("Unable to load ClusterInfo", local.ZapError(err))
			return nil, err
		}
		logger.Info("Fetched clusterInfo..")
	}

	vpcFileConfig := &vpcconfig.VPCFileConfig{
		VPCConfig:    conf.VPC,
		IKSConfig:    conf.IKS,
		ServerConfig: conf.Server,
	}

	// Prepare provider registry
	registry, err := provider_util.InitProviders(vpcFileConfig, k8sClient, logger)
	if err != nil {
		logger.Error("Error configuring providers", local.ZapError(err))
		return nil, err
	}

	var providerName string
	if conf.IKS.Enabled {
		providerName = conf.IKS.IKSFileProviderName
	} else if conf.VPC.Enabled {
		providerName = conf.VPC.VPCVolumeType
	}
	cloudProvider := &IBMCloudStorageProvider{
		ProviderName:   providerName,
		ProviderConfig: conf,
		Registry:       registry,
		ClusterID:      clusterInfo.ClusterID,
	}
	logger.Info("Successfully read provider configuration")
	return cloudProvider, nil
}

// GetProviderSession ...
func (icp *IBMCloudStorageProvider) GetProviderSession(ctx context.Context, logger *zap.Logger) (provider.Session, error) {
	logger.Info("IBMCloudStorageProvider-GetProviderSession...")

	prov, err := icp.Registry.Get(icp.ProviderName)
	if err != nil {
		logger.Error("Not able to get the said provider, might be its not registered", local.ZapError(err))
		return nil, err
	}

	// Populating vpcfileConfig which is used to open session
	vpcfileConfig := &vpcconfig.VPCFileConfig{
		VPCConfig:    icp.ProviderConfig.VPC,
		IKSConfig:    icp.ProviderConfig.IKS,
		ServerConfig: icp.ProviderConfig.Server,
	}

	session, _, err := provider_util.OpenProviderSessionWithContext(ctx, prov, vpcfileConfig, icp.ProviderName, logger)
	if err == nil {
		logger.Info("Successfully got the provider session", zap.Reflect("ProviderName", session.ProviderName()))
		return session, nil
	}
	logger.Error("Failed to get provider session", zap.Reflect("Error", err))
	return nil, err
}

// GetConfig ...
func (icp *IBMCloudStorageProvider) GetConfig() *config.Config {
	return icp.ProviderConfig
}

// GetClusterID ...
func (icp *IBMCloudStorageProvider) GetClusterID() string {
	return icp.ClusterID
}
