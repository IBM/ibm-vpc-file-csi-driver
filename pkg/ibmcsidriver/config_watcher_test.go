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
	"testing"

	k8sUtils "github.com/IBM/secret-utils-lib/pkg/k8s_utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes/fake"
)

/* func TestWatchClusterConfigMap(t *testing.T) {
	var server *ghttp.Server
	logger, _ := GetTestLogger(t)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	clientset := fake.NewSimpleClientset()
	eventInterface := clientset.CoreV1().Events("")
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: eventInterface})

	pvw := &ConfigWatcher{
		logger:  logger,
		kclient: clientset,
	}

	testCases := []struct {
		testCaseName string
	}{
		{
			testCaseName: "User tags- success",
		},
		{
			testCaseName: "No user tags- success",
		},
	}
	for _, testcase := range testCases {
		//start test http server
		server = ghttp.NewServer()
		server.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest(http.MethodGet, "/v3/tags"),
				ghttp.RespondWith(http.StatusOK, `
                           {
                            "items": {
                            }
                          }
                        `),
			),
		)
		t.Run(testcase.testCaseName, func(t *testing.T) {
			volCRN, tags := pvw.getTags(testcase.pv, logger)
			expectedTagNum := 7
			if len(testcase.tags) > 0 {
				expectedTagNum = 9
			}
			assert.Equal(t, expectedTagNum, len(tags))
			assert.Equal(t, "test-volcrn", volCRN)
			vol := pvw.getVolume(pv, logger)
			assert.Equal(t, 1, *vol.Capacity)
			assert.Equal(t, "3000", *vol.Iops)
			assert.Equal(t, "test-volumeid", vol.VolumeID)
			assert.NotNil(t, vol.Attributes)
			assert.Equal(t, "12345", vol.Attributes[strings.ToLower(utils.ClusterIDLabel)])

			pvw.updateSubnetList(testcase.pv, testcase.pv)
		})
	}
} */

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
			WatchClusterConfigMap(kubernetesClient, logger)
		})
	}
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
