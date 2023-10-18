/**
 *
 * Copyright 2021- IBM Inc. All rights reserved
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

//Package ibmcsidriver ...
package ibmcsidriver

import (
	"strings"
	"time"

	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const CONFIGMAP_NAME = "ibm-cloud-provider-data"
const CONFIGMAP_NAMESPACE = "kube-system"
const CONFIG_DATA_KEY = "vpc_subnet_ids"

var VPC_SUBNET_IDS string

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
	watchlist := cache.NewListWatchFromClient(cw.kclient.CoreV1().RESTClient(), "configmaps", CONFIGMAP_NAMESPACE, fields.Everything())
	_, controller := cache.NewInformer(watchlist, &v1.ConfigMap{}, time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    nil,
			DeleteFunc: nil,
			UpdateFunc: cw.updateSubnetList,
		},
	)
	cw.logger.Debug("ConfigWatcher starting")
	stopch := wait.NeverStop
	go controller.Run(stopch)
	cw.logger.Info("ConfigWatcher started")
	<-stopch
}

func (cw *ConfigWatcher) updateSubnetList(oldObj, newObj interface{}) {
	newData, _ := newObj.(*v1.ConfigMap)
	oldData, _ := oldObj.(*v1.ConfigMap)
	if strings.TrimSpace(newData.Name) == CONFIGMAP_NAME {
		newConfig := newData.Data[CONFIG_DATA_KEY]
		oldConfig := oldData.Data[CONFIG_DATA_KEY]
		if newConfig != "" && (newConfig != oldConfig) {
			VPC_SUBNET_IDS = newData.Data[CONFIG_DATA_KEY]
			cw.logger.Info("Updated the vpc subnet list ", zap.Any("VPC_SUBNET_IDS", VPC_SUBNET_IDS))
		}
	}
}

func WatchClusterConfigMap(log *zap.Logger) {
	client, err := getKubeClient(log)
	if err != nil {
		log.Error("no valid kubeclient")
	}
	configWatcher := NewConfigWatcher(client, log)
	if configWatcher != nil {
		go configWatcher.Start()
	} else {
		log.Warn("ConfigWatcher not started ")
	}

}

func getKubeClient(log *zap.Logger) (kubernetes.Interface, error) {

	// Fetching cluster config used to create k8s client
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	// Creating k8s client used to read secret
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}
