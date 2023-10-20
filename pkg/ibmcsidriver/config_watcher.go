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

	k8sUtils "github.com/IBM/secret-utils-lib/pkg/k8s_utils"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type ConfigWatcher struct {
	logger  *zap.Logger
	kclient kubernetes.Interface
}

func NewConfigWatcher(client kubernetes.Interface, log *zap.Logger) *ConfigWatcher {
	return &ConfigWatcher{
		logger:  log,
		kclient: client,
	}
}

func (cw *ConfigWatcher) Start() {
	watchlist := cache.NewListWatchFromClient(cw.kclient.CoreV1().RESTClient(), "configmaps", ConfigmapNamespace, fields.Set{"metadata.name": ConfigmapName}.AsSelector())
	_, controller := cache.NewInformer(watchlist, &v1.ConfigMap{}, time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    nil,
			DeleteFunc: nil,
			UpdateFunc: cw.updateSubnetList,
		},
	)
	cw.logger.Info("ConfigWatcher starting - start watching for any updates in subnet list", zap.Any("configmap name", ConfigmapName), zap.Any("configmap namespace", ConfigmapNamespace))
	stopch := wait.NeverStop
	go controller.Run(stopch)
	cw.logger.Info("ConfigWatcher started...")
	<-stopch
}

// updateSubnetList - Updates the VPC_SUBNET_IDS when ibm-cloud-provider-data configmap is updated.
func (cw *ConfigWatcher) updateSubnetList(oldObj, newObj interface{}) {
	newData, _ := newObj.(*v1.ConfigMap)
	oldData, _ := oldObj.(*v1.ConfigMap)
	// Confirm if the event recieved is for ibm-cloud-provider-data configmap or not.
	if strings.TrimSpace(newData.Name) == ConfigmapName {
		newConfig := newData.Data[ConfigmapDataKey]
		oldConfig := oldData.Data[ConfigmapDataKey]
		// Env variable VPC_SUBNET_IDS will be updated only when there is
		// non empty data and there is change in configmap
		if newConfig != "" && (newConfig != oldConfig) {
			VPC_SUBNET_IDS := newData.Data[ConfigmapDataKey]
			os.Setenv("VPC_SUBNET_IDS", VPC_SUBNET_IDS)
			cw.logger.Info("Updated the vpc subnet list ", zap.Any("VPC_SUBNET_IDS", VPC_SUBNET_IDS))
		}
	}
}

func WatchClusterConfigMap(client k8sUtils.KubernetesClient, log *zap.Logger) {
	configWatcher := NewConfigWatcher(client.Clientset, log)
	if configWatcher == nil {
		log.Error("ConfigWatcher not started ")
		return
	}
	go configWatcher.Start()
}
