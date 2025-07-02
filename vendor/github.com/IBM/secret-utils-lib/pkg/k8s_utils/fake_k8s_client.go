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

// Package k8s_utils ...
package k8s_utils

import (
	"context"
	"errors"
	"io/ioutil"

	"github.com/IBM/secret-utils-lib/pkg/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// FakeGetk8sClientSet ...
func FakeGetk8sClientSet() (KubernetesClient, error) {
	return KubernetesClient{Namespace: "kube-system", Clientset: fake.NewSimpleClientset()}, nil
}

// FakeCreateSecret ...
func FakeCreateSecret(kc KubernetesClient, fakeAuthType, secretdatafilepath string) error {
	secret := new(v1.Secret)

	var dataname string
	switch fakeAuthType {
	case utils.IAM, utils.PODIDENTITY:
		secret.Name = utils.IBMCLOUD_CREDENTIALS_SECRET
		dataname = utils.CLOUD_PROVIDER_ENV
	case utils.DEFAULT:
		secret.Name = utils.STORAGE_SECRET_STORE_SECRET
		dataname = utils.SECRET_STORE_FILE
	case "invalid":
		secret.Name = utils.IBMCLOUD_CREDENTIALS_SECRET
	default:
		return errors.New("undefined auth type")
	}

	secret.Namespace = kc.Namespace
	data := make(map[string][]byte)

	byteData, err := ioutil.ReadFile(secretdatafilepath)
	if err != nil {
		return err
	}

	data[dataname] = byteData
	secret.Data = data
	_, err = kc.Clientset.CoreV1().Secrets(kc.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// FakeCreateSecretWithKey ...
func FakeCreateSecretWithKey(kc KubernetesClient, secretName, dataName, secretdatafilepath string) error {
	secret := new(v1.Secret)
	secret.Name = secretName

	secret.Namespace = kc.Namespace
	data := make(map[string][]byte)

	byteData, err := ioutil.ReadFile(secretdatafilepath)
	if err != nil {
		return err
	}

	data[dataName] = byteData
	secret.Data = data
	_, err = kc.Clientset.CoreV1().Secrets(kc.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// FakeCreateCM ...
func FakeCreateCM(kc KubernetesClient, clsuterInfofilepath string) error {
	byteData, err := ioutil.ReadFile(clsuterInfofilepath)
	if err != nil {
		return err
	}

	data := make(map[string]string)
	data["cluster-config.json"] = string(byteData)
	cm := new(v1.ConfigMap)
	cm.Data = data
	cm.Name = "cluster-info"

	_, err = kc.Clientset.CoreV1().ConfigMaps(kc.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}
