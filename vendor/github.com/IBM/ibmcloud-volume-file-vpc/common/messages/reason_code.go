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

// Package messages ...
package messages

const (
	//AuthenticationFailed indicate authentication to IAM endpoint failed. e,g IAM_TOKEN refresh
	AuthenticationFailed = "AuthenticationFailed"
	//CreateVolumeAccessPointFailed indicates if create volume access point failed
	CreateVolumeAccessPointFailed = "CreateVolumeAccessPointFailed"
	//DeleteVolumeAccessPointFailed indicates if delete volume access point from instance is failed
	DeleteVolumeAccessPointFailed = "DeleteVolumeAccessPointFailed"
	//AccessPointWithVPCIDFindFailed indicates if the volume access point is not found with given request VPCID
	AccessPointWithVPCIDFindFailed = "AccessPointWithVPCIDFindFailed"
	//AccessPointWithAPIDFindFailed indicates if the volume access point is not found with given request access point ID
	AccessPointWithAPIDFindFailed = "AccessPointWithAPIDFindFailed"
	//CreateVolumeAccessPointTimedOut indicates the create volume access point is not completed within the specified time out
	CreateVolumeAccessPointTimedOut = "CreateVolumeAccessPointTimedOut"
	//DeleteVolumeAccessPointTimedOut indicates the delete volume access point is not completed within the specified time out
	DeleteVolumeAccessPointTimedOut = "DeleteVolumeAccessPointTimedOut"
	//VolumeAccessPointExist indicates that volume cannot be deleted as there exist volume access point
	VolumeAccessPointExist = "VolumeAccessPointExist"
)
