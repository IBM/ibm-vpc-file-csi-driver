/**
 *
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache2.0
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

// Package ibmcsidriver ...
package ibmcsidriver

import (
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type ConfigWatcher struct {
	logger *zap.Logger
	client rest.Interface
}

func NewConfigWatcher(client rest.Interface, log *zap.Logger) *ConfigWatcher {
	return &ConfigWatcher{
		logger: log,
		client: client,
	}
}

func (cw *ConfigWatcher) Start() {
	watchlist := cache.NewListWatchFromClient(cw.client, "configmaps", ConfigmapNamespace, fields.Set{"metadata.name": ConfigmapName}.AsSelector())
	informerOptions := cache.InformerOptions{	
		ListerWatcher: watchlist,
		ObjectType:      &v1.ConfigMap{},
		ResyncPeriod:       time.Second * 0,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    nil,
			DeleteFunc: nil,
			UpdateFunc: cw.updateSubnetList,
		},
	}
	_, controller := cache.NewInformerWithOptions(informerOptions)
	stopch := wait.NeverStop
	go controller.Run(stopch)
	cw.logger.Info("ConfigWatcher started - start watching for any updates in subnet list", zap.Any("configmap name", ConfigmapName), zap.Any("configmap namespace", ConfigmapNamespace))
	<-stopch
}

// updateSubnetList - Updates the VPC_SUBNET_IDS when ibm-cloud-provider-data configmap is updated.
func (cw *ConfigWatcher) updateSubnetList(oldObj, newObj interface{}) {
	newData, _ := newObj.(*v1.ConfigMap)
	oldData, _ := oldObj.(*v1.ConfigMap)
	// Confirm if the event recieved is for ibm-cloud-provider-data configmap or not.
	if strings.TrimSpace(newData.Name) == ConfigmapName {
		newSubnetList := newData.Data[ConfigmapDataKey]
		oldSubnetList := oldData.Data[ConfigmapDataKey]
		// Env variable VPC_SUBNET_IDS will be updated only when there is
		// non empty data and there is change in configmap
		if newSubnetList != "" && (newSubnetList != oldSubnetList) {
			err := os.Setenv("VPC_SUBNET_IDS", newSubnetList)
			if err != nil {
				cw.logger.Warn("Error updating the subnet list..", zap.Any("Subnet list update request", newSubnetList), zap.Error(err))
				return
			}
			cw.logger.Info("Updated the vpc subnet list ", zap.Any("VPC_SUBNET_IDS", newSubnetList))
		}
	}
}

func WatchClusterConfigMap(client rest.Interface, log *zap.Logger) {
	configWatcher := NewConfigWatcher(client, log)
	go configWatcher.Start()
}
