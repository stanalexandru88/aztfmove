package state

import (
	"fmt"
	"strings"
)

type ResourceInstanceSummary struct {
	AzureID       string
	TerraformID   string
	FutureAzureID string
	Type          string
}

type ResourcesInstanceSummary []ResourceInstanceSummary

func (ris ResourcesInstanceSummary) NotSupported() []string {
	var IDs []string
	for _, r := range ris {
		if contains(resourcesNotSupportedInAzure, r.Type) {
			IDs = append(IDs, r.TerraformID)
		}
	}
	return IDs
}

func (ris ResourcesInstanceSummary) NoMovementNeeded() []string {
	var IDs []string
	for _, r := range ris {
		if contains(resourcesNotNeedingMovement, r.Type) {
			IDs = append(IDs, r.TerraformID)
		}
	}
	return IDs
}

func (ris ResourcesInstanceSummary) BlockingMovement() ([]string, []string) {
	var tfIDs []string
	var azureIDs []string
	for _, r := range ris {
		if contains(resourcesBlockingMovement, r.Type) {
			tfIDs = append(tfIDs, r.TerraformID)
			azureIDs = append(azureIDs, r.AzureID)
		}
	}
	return tfIDs, azureIDs
}

func (ris ResourcesInstanceSummary) MovableOnAzure() []string {
	var IDs []string
	for _, r := range ris {
		if !contains(resourcesNotSupportedInAzure, r.Type) && !contains(resourcesOnlyMovedInTF, r.Type) && !contains(resourcesBlockingMovement, r.Type) && !contains(resourcesNotNeedingMovement, r.Type) {

			if !contains(IDs, r.AzureID) && !strings.HasPrefix(r.AzureID, "https://") {
				IDs = append(IDs, r.AzureID)
		}

		}
	}
	return IDs
}

func (ris ResourcesInstanceSummary) ToCorrectInTFState() map[string]string {
	IDs := make(map[string]string)
	for _, r := range ris {
		if !contains(resourcesNotSupportedInAzure, r.Type) && !contains(resourcesBlockingMovement, r.Type) && !contains(resourcesNotNeedingMovement, r.Type) {
			IDs[r.TerraformID] = r.FutureAzureID
		}
	}
	return IDs
}

var resourcesOnlyMovedInTF = []string{
	"azurerm_app_service_slot",
	"azurerm_app_service_slot_virtual_network_swift_connection",
	"azurerm_key_vault_access_policy",
	"azurerm_key_vault_secret",
	"azurerm_mssql_database",
	"azurerm_mssql_database_extended_auditing_policy",
	"azurerm_mysql_firewall_rule",
	"azurerm_sql_firewall_rule",
	"azurerm_storage_container",
	"azurerm_storage_share",
	"azurerm_subnet",
	"azurerm_subnet_network_security_group_association",
}

var resourcesBlockingMovement = []string{
	"azurerm_app_service_virtual_network_swift_connection",
	"azurerm_app_service_slot_virtual_network_swift_connection",
}

var resourcesNotSupportedInAzure = []string{
	"azurerm_client_config",
	"azurerm_kubernetes_cluster",
	"azurerm_monitor_diagnostic_setting",
	"azurerm_monitor_metric_alert",
	"azurerm_resource_group",
}

var resourcesNotNeedingMovement = []string{
	"azurerm_storage_share_file", // lacks any reference to subscription/resource group and is a child resource, so no need to convert and/or move
	"azurerm_storage_blob",    
}

func (tfstate TerraformState) Filter(resourceFilter, moduleFilter, resourceGroupFilter, sourceSubscriptionID, targetResourceGroup, targetSubscriptionID string) (resourceInstances ResourcesInstanceSummary, sourceResourceGroup string, err error) {
	resourceGroup := resourceGroupFilter
	for _, r := range tfstate.Resources {
		if !strings.Contains(r.Provider, "provider[\"registry.terraform.io/hashicorp/azurerm\"]") || r.Mode != "managed" {
			continue
		}

		// first filter: resource
		if (resourceFilter != "" && resourceFilter != "*") && r.ID() != resourceFilter {
			continue
		}

		// second filter: module
		if (moduleFilter != "" && moduleFilter != "*") && r.Module != moduleFilter {
			continue
		}

		// third filter: resourcesNotNeedingMovement
		if contains(resourcesNotNeedingMovement, r.Type) {
			for _, instance := range r.Instances {
				summary := ResourceInstanceSummary{
					AzureID:       instance.Attributes.ID,
					FutureAzureID: instance.Attributes.ID,
					TerraformID:   instance.ID(r),
					Type:          r.Type,
				}
				resourceInstances = append(resourceInstances, summary)
			}
			continue
		}

		for _, instance := range r.Instances {
			if instance.SubscriptionID() == "" {
				err = fmt.Errorf("subscription ID is not found for %s. Please file a PR on https://github.com/aristosvo/aztfmove and mention this ID: %s", instance.ID(r), instance.ID(r))
				return nil, "", err
			}

			// Only one subscription is supported at the same time
			if instance.SubscriptionID() != sourceSubscriptionID {
				err = fmt.Errorf("resource instance `%s` has a different subscription specified, unable to start moving. Resource instance subscription ID: %s, specified subscription ID: %s", instance.ID(r), strings.Split(instance.Attributes.ID, "/")[2], sourceSubscriptionID)
				return nil, "", err
			}

			instanceResourceGroup := instance.ResourceGroup()
			if instanceResourceGroup == "" && !contains(resourcesNotSupportedInAzure, r.Type) && !contains(resourcesBlockingMovement, r.Type) {
				err = fmt.Errorf("resource group is not found for %s. Please file a PR on https://github.com/aristosvo/aztfmove and mention this ID: %s", instance.ID(r), instance.ID(r))
				return nil, "", err
			}

			// thirth filter: resource group
			if resourceGroupFilter != "*" && instanceResourceGroup != resourceGroupFilter {
				continue
			}

			// Only one resource group is supported at the same time
			if resourceGroup == "*" && !contains(resourcesNotSupportedInAzure, r.Type) && !contains(resourcesBlockingMovement, r.Type) {
				resourceGroup = instanceResourceGroup
			} else if resourceGroup != instanceResourceGroup && !contains(resourcesNotSupportedInAzure, r.Type) && !contains(resourcesBlockingMovement, r.Type) {

				err = fmt.Errorf("multiple resource groups found within your selection, unable to start moving. Resource groups found: [%s, %s]", resourceGroup, instanceResourceGroup)
				return nil, "", err
			}

			if instance.SubscriptionID() == targetSubscriptionID && instanceResourceGroup == targetResourceGroup && !contains(resourcesNotSupportedInAzure, r.Type) && !contains(resourcesBlockingMovement, r.Type) {
				err = fmt.Errorf("the selected resource %s is already in the target resource group", instance.ID(r))
				return nil, "", err
			}

			// Prepare formatting of ID after movement. Maybe this could be extracted from the movement response?
			// IDs which are formatted like /subscriptions/*/resourceGroups/* are considered sensitive for movement, IDs like https://example.blob.core.windows.net/container not
			resourceGroupId := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", sourceSubscriptionID, resourceGroup)
			targetResourceGroupId := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", targetSubscriptionID, targetResourceGroup)
			futureAzureId := instance.Attributes.ID
			if strings.HasPrefix(instance.Attributes.ID, resourceGroupId) {
				futureAzureId = strings.Replace(instance.Attributes.ID, resourceGroupId, targetResourceGroupId, 1)
			}

			summary := ResourceInstanceSummary{
				AzureID:       instance.Attributes.ID,
				FutureAzureID: futureAzureId,
				TerraformID:   instance.ID(r),
				Type:          r.Type,
			}
			resourceInstances = append(resourceInstances, summary)
		}
	}
	return resourceInstances, resourceGroup, nil
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}
