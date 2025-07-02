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

// Package vpcvolume ...
package vpcfilevolume

import (
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
)

const (
	//IksV2PathPrefix ...
	IksV2PathPrefix = "v2/storage/"
)

// IKSVolumeService ...
type IKSVolumeService struct {
	FileShareService
	pathPrefix    string
	receiverError error
}

var _ FileShareManager = &IKSVolumeService{}

// NewIKSVolumeService ...
func NewIKSVolumeService(client client.SessionClient) FileShareManager {
	err := models.IksError{}
	iksVolumeService := &IKSVolumeService{
		FileShareService: FileShareService{
			client: client,
		},
		pathPrefix:    IksV2PathPrefix,
		receiverError: &err,
	}
	return iksVolumeService
}
