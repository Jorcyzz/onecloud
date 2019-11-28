// Copyright 2019 Yunion
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package compute

type CloudproviderDetails struct {
	Provider         string `json:"provider,omitempty"`
	Brand            string `json:"brand,omitempty"`
	Account          string `json:"account,omitempty"`
	AccountId        string `json:"account_id,omitempty"`
	Manager          string `json:"manager,omitempty"`
	ManagerId        string `json:"manager_id,omitempty"`
	ManagerProject   string `json:"manager_project,omitempty"`
	ManagerProjectId string `json:"manager_project_id,omitempty"`
	ManagerDomain    string `json:"manager_domain,omitempty"`
	ManagerDomainId  string `json:"manager_domain_id,omitempty"`
	Region           string `json:"region,omitempty"`
	RegionId         string `json:"region_id,omitempty"`
	CloudregionId    string `json:"cloudregion_id,omitempty"`
	RegionExternalId string `json:"region_external_id,omitempty"`
	RegionExtId      string `json:"region_ext_id,omitempty"`
	Zone             string `json:"zone,omitempty"`
	ZoneId           string `json:"zone_id,omitempty"`
	ZoneExtId        string `json:"zone_ext_id,omitempty"`
	CloudEnv         string `json:"cloud_env,omitempty"`
}

type CloudaccountListInput struct {
	// List objects belonging to the cloud provider
	Cloudprovider string `json:"cloudprovider"`

	// List objects belonging to the cloud account
	Cloudaccount string `json:"cloudprovider"`

	// List objects from the providers, choices:"OneCloud|VMware|Aliyun|Qcloud|Azure|Aws|Huawei|OpenStack|Ucloud|ZStack|Google"
	Providers []string `json:"providers"`

	// List objects belonging to brands
	Brands []string `json:"brands"`
}

type CloudTypeListInput struct {
	// enum: public_cloud,private_cloud,on_premise
	CloudEnv string `json:"cloud_env"`

	// List objects managed by external providers
	// default: false
	IsManaged bool `json:"is_managed"`
}
