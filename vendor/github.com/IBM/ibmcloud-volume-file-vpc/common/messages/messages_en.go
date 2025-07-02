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

import (
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
)

// messagesEn ...
var messagesEn = map[string]util.Message{
	"ErrorRequiredFieldMissing": {
		Code:        "ErrorRequiredFieldMissing",
		Description: "[%s] is required to complete the operation.",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Review the error that is returned. Provide the missing information in your request and try again.",
	},
	"FailedToPlaceOrder": {
		Code:        "FailedToPlaceOrder",
		Description: "Failed to create file share with the storage provider",
		Type:        util.ProvisioningFailed,
		RC:          500,
		Action:      "Review the error that is returned. If the file share creation service is currently unavailable, try to manually create the file share with the 'ibmcloud is share-create' command.",
	},
	"FailedToDeleteVolume": {
		Code:        "FailedToDeleteVolume",
		Description: "The file share ID '%d' could not be deleted from your VPC.",
		Type:        util.DeletionFailed,
		RC:          500,
		Action:      "Verify that the file share ID exists. Run 'ibmcloud is shares' to list available file shares in your account. If the ID is correct, try to delete the file share with the 'ibmcloud is share-delete' command. ",
	},
	"FailedToExpandVolume": {
		Code:        "FailedToExpandVolume",
		Description: "The volume ID '%s' could not be expanded from your VPC.",
		Type:        util.ExpansionFailed,
		RC:          500,
		Action:      "Verify that the volume ID exists. If the ID is correct, check that expected capacity is valid and supported",
	},
	"FailedToUpdateVolume": {
		Code:        "FailedToUpdateVolume",
		Description: "The file share ID '%d' could not be updated",
		Type:        util.UpdateFailed,
		RC:          500,
		Action:      "Verify that the file share ID exists. Run 'ibmcloud is shares' to list available file share in your account.",
	},
	"StorageFindFailedWithVolumeId": {
		Code:        "StorageFindFailedWithVolumeId",
		Description: "A file share with the specified file share ID '%s' could not be found.",
		Type:        util.RetrivalFailed,
		RC:          404,
		Action:      "Verify that the file share ID exists. Run 'ibmcloud is shares' to list available file shares in your account.",
	},
	"StorageFindFailedWithVolumeName": {
		Code:        "StorageFindFailedWithVolumeName",
		Description: "A file share with the specified file share name '%s' does not exist.",
		Type:        util.RetrivalFailed,
		RC:          404,
		Action:      "Verify that the specified file share exists. Run 'ibmcloud is shares' to list available file shares in your account.",
	},
	"AccessPointWithAPIDFindFailed": {
		Code:        AccessPointWithAPIDFindFailed,
		Description: "No mount target could be found for the specified file share ID '%s' and mount target ID '%s'",
		Type:        util.VolumeAccessPointFindFailed,
		RC:          400,
		Action:      "Verify that a mount target for your file share exists. Run `ibmcloud is share-targets <SHARE_ID>` to list all mount targets for a file share. Check if file share ID and mount target ID is valid",
	},
	"AccessPointWithVPCIDFindFailed": {
		Code:        AccessPointWithVPCIDFindFailed,
		Description: "No mount target could be found for the specified file share ID '%s' and VPC ID %s",
		Type:        util.VolumeAccessPointFindFailed,
		RC:          400,
		Action:      "Verify that a mount target for your file share exists. Run `ibmcloud is share-targets <SHARE_ID>` to list all volume targets for a file share. Check if file share ID and VPC ID is valid",
	},
	"CreateVolumeAccessPointFailed": {
		Code:        CreateVolumeAccessPointFailed,
		Description: "The file share ID '%s' could not create mount target for VPC ID %s.",
		Type:        util.CreateVolumeAccessPointFailed,
		RC:          500,
		Action:      "Verify that the file share ID and VPC ID exist.",
	},
	"CreateVolumeAccessPointTimedOut": {
		Code:        CreateVolumeAccessPointTimedOut,
		Description: "The file share ID '%s' could not create mount target ID '%s'.",
		Type:        util.CreateVolumeAccessPointFailed,
		RC:          500,
		Action:      "Verify that the file share ID exists.",
	},
	"DeleteVolumeAccessPointFailed": {
		Code:        DeleteVolumeAccessPointFailed,
		Description: "The file share ID '%s' could not delete mount target ID '%s'.",
		Type:        util.DeleteVolumeAccessPointFailed,
		RC:          500,
		Action:      "Verify that the specified file share ID has active mount target.",
	},
	"DeleteVolumeAccessPointTimedOut": {
		Code:        DeleteVolumeAccessPointTimedOut,
		Description: "The file share ID '%s' could not delete mount target ID '%s'",
		Type:        util.DeleteVolumeAccessPointFailed,
		RC:          500,
		Action:      "Verify that the specified file share ID has active mount targets.",
	},
	"InvalidVolumeID": {
		Code:        "InvalidVolumeID",
		Description: "The specified file share ID '%s' is not valid.",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Verify that the file share ID exists. Run 'ibmcloud is shares' to list available file shares in your account.",
	},
	"InvalidVolumeName": {
		Code:        "InvalidVolumeName",
		Description: "The specified file share name '%s' is not valid. ",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Verify that the file share name exists. Run 'ibmcloud is shares' to list available file shares in your account.",
	},
	"VolumeCapacityInvalid": {
		Code:        "VolumeCapacityInvalid",
		Description: "The specified file share capacity '%d' is not valid. ",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Verify the specified file share capacity. The file share capacity must be a positive number between 10 GB and maximum allowed value for the respective storage profile. Refer IBM Cloud File Storage for VPC documentation https://cloud.ibm.com/docs/vpc?topic=vpc-file-storage-profiles.",
	},
	"EmptyResourceGroup": {
		Code:        "EmptyResourceGroup",
		Description: "Resource group information could not be found.",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Provide the name or ID of the resource group that you want to use for your file share. Run 'ibmcloud resource groups' to list the resource groups that you have access to. ",
	},
	"EmptyResourceGroupIDandName": {
		Code:        "EmptyResourceGroupIDandName",
		Description: "Resource group ID or name could not be found.",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Provide the name or ID of the resource group that you want to use for your file share. Run 'ibmcloud resource groups' to list the resource groups that you have access to.",
	},
	"VolumeNotInValidState": {
		Code:        "VolumeNotInValidState",
		Description: "Share %s did not get valid (stable) status within timeout period.",
		Type:        util.ProvisioningFailed,
		RC:          500,
		Action:      "Run 'ibmcloud is share <SHARE-ID>' and check the current status. If the status is not stable, contact support.",
	},
	"SubnetsListFailed": {
		Code:        "SubnetsListFailed",
		Description: "Unable to fetch list of subnet.",
		Type:        util.RetrivalFailed,
		RC:          500,
		Action:      "Unable to list subnet. Target to appropriate region 'ibmcloud target -r <region>' and verify if 'ibmcloud is subnets' is returning the subnets. If it is not returning then raise ticket for VPC team else raise ticket for IKS team.",
	},
	"SubnetFindFailed": {
		Code:        "SubnetFindFailed",
		Description: "A subnet with the specified zone '%s' and available cluster subnet list '%s' could not be found.",
		Type:        util.RetrivalFailed,
		RC:          404,
		Action:      "Check whether your VPC and cluster are in different resource groups. If your VPC and cluster are in different resource groups, then refer to https://cloud.ibm.com/docs/containers?topic=containers-storage-file-vpc-apps#storage-file-vpc-custom-sc for more details. If your VPC and cluster are in the same resource group, contact support.",
	},
	"SecurityGroupsListFailed": {
		Code:        "SecurityGroupsListFailed",
		Description: "Unable to fetch list of securityGroup.",
		Type:        util.RetrivalFailed,
		RC:          500,
		Action:      "Unable to list securityGroup. Target to appropriate region 'ibmcloud target -r <region>' and verify if 'ibmcloud is securityGroups' is returning the securityGroups. If it is not returning then raise ticket for VPC team else raise ticket for IKS team.",
	},
	"SecurityGroupFindFailed": {
		Code:        "SecurityGroupFindFailed",
		Description: "A securityGroup with the specified securityGroup name '%s' could not be found.",
		Type:        util.RetrivalFailed,
		RC:          404,
		Action:      "Verify that the cluster securityGroup exists. Target to appropriate region 'ibmcloud target -r <region>' and verify if 'ibmcloud is securityGroups' is returning the securityGroups. Please provide the output and raise ticket for IKS team.",
	},
	"ListVolumesFailed": {
		Code:        "ListVolumesFailed",
		Description: "Unable to fetch list of file shares.",
		Type:        util.RetrivalFailed,
		RC:          404,
		Action:      "Unable to list file shares. Run 'ibmcloud is shares' to list available file shares in your account.",
	},
	"InvalidListVolumesLimit": {
		Code:        "InvalidListVolumesLimit",
		Description: "The value '%v' specified in the limit parameter of the list file share call is not valid.",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Verify the limit parameter's value. The limit must be a positive number between 0 and 100.",
	},
	"StartVolumeIDNotFound": {
		Code:        "StartVolumeIDNotFound",
		Description: "The file share ID '%s' specified in the start parameter of the list volume call could not be found.",
		Type:        util.InvalidRequest,
		RC:          400,
		Action:      "Please verify that the start file share ID is correct and whether you have access to the file share ID.",
	},
	VolumeAccessPointExist: {
		Code:        VolumeAccessPointExist,
		Description: "The file share ID '%s' could not be deleted from your VPC. File share has mount targets which needs to deleted. Please go through the list of VPCs = '%v'",
		Type:        util.DeletionFailed,
		Action:      "User need to review all mount targets and delete them first before deleting file share. Run `ibmcloud is share-targets <SHARE_ID>` to get the mount target for the file share. ",
	},
}

// InitMessages ...
func InitMessages() map[string]util.Message {
	return messagesEn
}
