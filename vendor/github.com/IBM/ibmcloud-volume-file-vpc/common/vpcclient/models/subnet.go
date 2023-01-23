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

// VPC ...
type VPC struct {
	Href string `json:"href,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Subnet ...
type Subnet struct {
	Href string `json:"href,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`

	CRN           string         `json:"crn,omitempty"`
	ResourceGroup *ResourceGroup `json:"resource_group,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
	VPC           *provider.VPC  `json:"vpc,omitempty"`
	Zone          *Zone          `json:"zone,omitempty"`
}

//SubnetRef ...
type SubnetRef struct {
	ID string `json:"id,omitempty"`
}

// SubnetList ...
type SubnetList struct {
	Next    string    `json:"next,omitempty"`
	Subnets []*Subnet `json:"subnets,omitempty"`
	Limit   int       `json:"limit,omitempty"`
}

// ListSubnetFilters ...
type ListSubnetFilters struct {
	ResourceGroupID string `json:"resource_group.id,omitempty"`
}
