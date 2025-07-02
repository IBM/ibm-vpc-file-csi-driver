/**
 * Copyright 2024 IBM Corp.
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

	vpcauth "github.com/IBM/ibmcloud-volume-file-vpc/common/auth"
	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/riaas"
	vpcprovider "github.com/IBM/ibmcloud-volume-file-vpc/file/provider"
	vpcconfig "github.com/IBM/ibmcloud-volume-file-vpc/file/vpcconfig"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	utilReasonCode "github.com/IBM/ibmcloud-volume-interface/lib/utils/reasoncode"
	"github.com/IBM/ibmcloud-volume-interface/provider/local"
	"github.com/IBM/secret-utils-lib/pkg/k8s_utils"

	"go.uber.org/zap"
)

// IksVpcFileProvider  handles both IKS and  RIAAS sessions
type IksVpcFileProvider struct {
	vpcprovider.VPCFileProvider
	vpcFileProvider *vpcprovider.VPCFileProvider // Holds VPC provider. Requires to avoid recursive calls
	iksFileProvider *vpcprovider.VPCFileProvider // Holds IKS provider
}

var _ local.Provider = &IksVpcFileProvider{}

// NewProvider handles both IKS and  RIAAS sessions
func NewProvider(conf *vpcconfig.VPCFileConfig, k8sClient *k8s_utils.KubernetesClient, logger *zap.Logger) (local.Provider, error) {
	var err error
	//Setup vpc provider
	provider, err := vpcprovider.NewProvider(conf, k8sClient, logger)
	if err != nil {
		logger.Error("Error initializing VPC Provider", zap.Error(err))
		return nil, err
	}
	vpcFileProvider, _ := provider.(*vpcprovider.VPCFileProvider)

	// Setup IKS provider
	provider, err = vpcprovider.NewProvider(conf, k8sClient, logger)
	if err != nil {
		logger.Error("Error initializing IKS Provider", zap.Error(err))
		return nil, err
	}
	iksFileProvider, _ := provider.(*vpcprovider.VPCFileProvider)

	//Overrider Base URL
	iksFileProvider.APIConfig.BaseURL = conf.VPCConfig.IKSTokenExchangePrivateURL
	// Setup IKS-VPC dual provider
	iksVpcFileProvider := &IksVpcFileProvider{
		VPCFileProvider: *vpcFileProvider,
		vpcFileProvider: vpcFileProvider,
		iksFileProvider: iksFileProvider,
	}

	iksVpcFileProvider.iksFileProvider.ContextCF, err = vpcauth.NewVPCContextCredentialsFactory(iksVpcFileProvider.vpcFileProvider.Config, k8sClient)
	if err != nil {
		logger.Error("Error initializing context credentials factory", zap.Error(err))
		return nil, err
	}

	return iksVpcFileProvider, nil
}

// OpenSession opens a session on the provider
func (iksp *IksVpcFileProvider) OpenSession(ctx context.Context, contextCredentials provider.ContextCredentials, ctxLogger *zap.Logger) (provider.Session, error) {
	ctxLogger.Info("Entering IksVpcFileProvider.OpenSession")

	defer func() {
		ctxLogger.Debug("Exiting IksVpcFileProvider.OpenSession")
	}()
	ctxLogger.Info("Opening VPC file session")
	ccf, _ := iksp.vpcFileProvider.ContextCredentialsFactory(nil)
	ctxLogger.Info("Its IKS dual session. Getttng IAM token for  VPC file session")
	vpcContextCredentials, err := ccf.ForIAMAccessToken(iksp.iksFileProvider.Config.VPCConfig.G2APIKey, ctxLogger)
	if err != nil {
		ctxLogger.Error("Error occurred while generating IAM token for VPC", zap.Error(err))
		if util.ErrorReasonCode(err) == utilReasonCode.EndpointNotReachable {
			userErr := userError.GetUserError(string(userError.EndpointNotReachable), err)
			return nil, userErr
		}
		if util.ErrorReasonCode(err) == utilReasonCode.Timeout {
			userErr := userError.GetUserError(string(userError.Timeout), err)
			return nil, userErr
		}
		return nil, err
	}
	session, err := iksp.vpcFileProvider.OpenSession(ctx, vpcContextCredentials, ctxLogger)
	if err != nil {
		ctxLogger.Error("Error occurred while opening VPCSession", zap.Error(err))
		return nil, err
	}
	vpcSession, _ := session.(*vpcprovider.VPCSession)
	ctxLogger.Info("Opening IKS file session")

	ccf = iksp.iksFileProvider.ContextCF
	iksp.iksFileProvider.ClientProvider = riaas.IKSRegionalAPIClientProvider{}

	ctxLogger.Info("Its ISK dual session. Getttng IAM token for  IKS file session")
	iksContextCredentials, err := ccf.ForIAMAccessToken(iksp.iksFileProvider.Config.VPCConfig.G2APIKey, ctxLogger)
	if err != nil {
		ctxLogger.Warn("Error occurred while generating IAM token for IKS. But continue with VPC session alone. \n Share provisioning will work but cleanup on cluster deletion will failed.", zap.Error(err))
		session = &vpcprovider.VPCSession{
			Logger:       ctxLogger,
			SessionError: err,
		} // Empty session to avoid Nil references.
	} else {
		session, err = iksp.iksFileProvider.OpenSession(ctx, iksContextCredentials, ctxLogger)
		if err != nil {
			ctxLogger.Error("Error occurred while opening IKSSession", zap.Error(err))
		}
	}

	iksSession, _ := session.(*vpcprovider.VPCSession)

	// Setup Dual Session that handles for VPC and IKS connections
	vpcIksSession := IksVpcSession{
		VPCSession: *vpcSession,
		IksSession: iksSession,
	}
	ctxLogger.Debug("IksVpcSession", zap.Reflect("IksVpcSession", vpcIksSession))
	return &vpcIksSession, nil
}

// ContextCredentialsFactory ...
func (iksp *IksVpcFileProvider) ContextCredentialsFactory(zone *string) (local.ContextCredentialsFactory, error) {
	return iksp.iksFileProvider.ContextCF, nil
}
