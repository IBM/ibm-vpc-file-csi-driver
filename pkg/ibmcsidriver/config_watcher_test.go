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

// Package ibmcsidriver ...
package ibmcsidriver

import (
	"bytes"
	"os"
	"testing"

	k8sUtils "github.com/IBM/secret-utils-lib/pkg/k8s_utils"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	restfake "k8s.io/client-go/rest/fake"
)

// TestWatchClusterConfigMap ...
func TestWatchClusterConfigMap(t *testing.T) {
	// Creating test logger
	logger, teardown := GetTestLogger(t)

	defer teardown()
	testcases := []struct {
		testcasename  string
		expectedError error
	}{
		{
			testcasename:  "Success",
			expectedError: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.testcasename, func(t *testing.T) {
			clientset := fake.NewSimpleClientset()
			kubernetesClient := k8sUtils.KubernetesClient{
				Namespace: "kube-system",
				Clientset: clientset,
			}
			FakeCreateCM(kubernetesClient)
			c := new(restfake.RESTClient)
			WatchClusterConfigMap(c, logger)
		})
	}
}

func TestUpdateSubnetList(t *testing.T) {
	// Creating test logger
	logger, teardown := GetTestLogger(t)
	defer teardown()

	testcases := []struct {
		testCaseName     string
		oldConfigMap     *v1.ConfigMap
		newConfigMap     *v1.ConfigMap
		currentSubnetID  string
		expectedSubnetID string
	}{
		/*{
			testCaseName: "Same subentID",
			oldConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: ConfigmapName,
				},
				Data: map[string]string{
					ConfigmapDataKey: "subnetid-1",
				},
			},
			newConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: ConfigmapName,
				},
				Data: map[string]string{
					ConfigmapDataKey: "subnetid-1",
				},
			},
			currentSubnetID:  "subnetid-1",
			expectedSubnetID: "subnetid-1",
		},*/
		{
			testCaseName: "Different subnetID",
			oldConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: ConfigmapName,
				},
				Data: map[string]string{
					ConfigmapDataKey: "subnetid-1",
				},
			},
			newConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: ConfigmapName,
				},
				Data: map[string]string{
					ConfigmapDataKey: "subnetid-2",
				},
			},
			currentSubnetID:  "subnetid-1",
			expectedSubnetID: "subnetid-2",
		},
	}

	c := new(restfake.RESTClient)
	cw := NewConfigWatcher(c, logger)
	WatchClusterConfigMap(c, logger)

	for _, testcase := range testcases {
		t.Run(testcase.testCaseName, func(t *testing.T) {
			os.Setenv("VPC_SUBNET_IDS", testcase.currentSubnetID)
			defer os.Unsetenv("VPC_SUBNET_IDS")
			cw.updateSubnetList(testcase.oldConfigMap, testcase.newConfigMap)
			assert.Equal(t, testcase.expectedSubnetID, os.Getenv("VPC_SUBNET_IDS"))
		})
	}
}

// FakeCreateCM ...
func FakeCreateCM(kc k8sUtils.KubernetesClient) error {
	data := make(map[string]string)
	data["vpc_subnet_ids"] = "0757-229d3fcd-41da-42fc-a770-fddf5f415d9c,0767-d6884d9a-57ba-49a4-8914-4ac120b70a21"
	cm := new(v1.ConfigMap)
	cm.Data = data
	cm.Name = "ibm-cloud-provider-data"

	_, err := kc.Clientset.CoreV1().ConfigMaps(kc.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// GetTestLogger ...
func GetTestLogger(t *testing.T) (logger *zap.Logger, teardown func()) {
	atom := zap.NewAtomicLevel()
	atom.SetLevel(zap.DebugLevel)

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	buf := &bytes.Buffer{}

	logger = zap.New(
		zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(buf),
			atom,
		),
		zap.AddCaller(),
	)

	teardown = func() {
		_ = logger.Sync()
		if t.Failed() {
			t.Log(buf)
		}
	}
	return
}
