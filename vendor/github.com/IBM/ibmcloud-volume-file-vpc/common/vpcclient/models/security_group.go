/*******************************************************************************
 * IBM Confidential
 * OCO Source Materials
 * IBM Cloud Kubernetes Service, 5737-D43
 * (C) Copyright IBM Corp. 2023 All Rights Reserved.
 * The source code for this program is not published or otherwise divested of
 * its trade secrets, irrespective of what has been deposited with
 * the U.S. Copyright Office.
 ******************************************************************************/

// Package models ...
package models

import (
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
)

// SecurityGroup ...
type SecurityGroup struct {
	CRN           string         `json:"crn,omitempty"`
	Href          string         `json:"href,omitempty"`
	ID            string         `json:"id,omitempty"`
	Name          string         `json:"name,omitempty"`
	ResourceGroup *ResourceGroup `json:"resource_group,omitempty"`
	VPC           *provider.VPC  `json:"vpc,omitempty"`
}

// SecurityGroupList ...
type SecurityGroupList struct {
	First          *HReference     `json:"first,omitempty"`
	Next           *HReference     `json:"next,omitempty"`
	SecurityGroups []SecurityGroup `json:"security_groups,omitempty"`
	Limit          int             `json:"limit,omitempty"`
	TotalCount     int             `json:"total_count,omitempty"`
}

// ListSecurityGroupFilters ...
type ListSecurityGroupFilters struct {
	ResourceGroupID string `json:"resource_group.id,omitempty"`
	VPCID           string `json:"vpc.id,omitempty"`
}
