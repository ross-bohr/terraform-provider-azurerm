package web

import (
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/features"
	azSchema "github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/tf/schema"

	"github.com/Azure/azure-sdk-for-go/services/web/mgmt/2018-02-01/web"
	"github.com/hashicorp/go-azure-helpers/response"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/azure"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/validate"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/clients"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/services/relay"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/timeouts"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

func resourceArmAppServiceHybridConnection() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmAppServiceHybridConnectionCreateUpdate,
		Read:   resourceArmAppServiceHybridConnectionRead,
		Update: resourceArmAppServiceHybridConnectionCreateUpdate,
		Delete: resourceArmAppServiceHybridConnectionDelete,

		Importer: azSchema.ValidateResourceIDPriorToImport(func(id string) error {
			_, err := ParseAppServiceHybridConnectionID(id)
			return err
		}),

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
			Read:   schema.DefaultTimeout(5 * time.Minute),
			Update: schema.DefaultTimeout(30 * time.Minute),
			Delete: schema.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"app_service_name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validateAppServiceName,
			},
			"resource_group_name": azure.SchemaResourceGroupName(),
			"relay_id": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: relay.ValidateHybridConnectionID,
			},
			"hostname": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validate.NoEmptyStrings,
			},
			"port": {
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: validate.PortNumberOrZero,
			},
			"service_bus_namespace": {
				Type:     schema.TypeString,
				Required: true,
				ValidateFunc: validation.StringMatch(
					regexp.MustCompile("^[a-zA-Z][-a-zA-Z0-9]{0,100}[a-zA-Z0-9]$"),
					"The namespace can contain only letters, numbers, and hyphens. The namespace must start with a letter, and it must end with a letter or number.",
				),
			},
			"service_bus_suffix": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      ".servicebus.windows.net",
				ValidateFunc: validate.NoEmptyStrings,
			},
			"send_key_name": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validate.NoEmptyStrings,
			},
			"send_key_value": {
				Type:         schema.TypeString,
				Required:     true,
				Sensitive:    true,
				ValidateFunc: validate.NoEmptyStrings,
			},
			"namespace_name": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"relay_name": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceArmAppServiceHybridConnectionCreateUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Web.AppServicesClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("app_service_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)
	relayArmURI := d.Get("relay_id").(string)
	relayId, err := relay.ParseHybridConnectionID(relayArmURI)
	if err != nil {
		return fmt.Errorf("Error parsing relay ID %q: %s", relayArmURI, err)
	}
	namespaceName := relayId.NamespaceName
	relayName := relayId.Name

	if features.ShouldResourcesBeImported() && d.IsNewResource() {
		existing, err := client.GetHybridConnection(ctx, resourceGroup, name, namespaceName, relayName)
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing App Service Hybrid Connection %q (Resource Group %q, Namespace %q, Relay Name %q): %s", name, resourceGroup, namespaceName, relayName, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_app_service_hybrid_connection", *existing.ID)
		}
	}

	port := int32(d.Get("port").(int))

	connectionEnvelope := web.HybridConnection{
		HybridConnectionProperties: &web.HybridConnectionProperties{
			ServiceBusNamespace: utils.String(d.Get("service_bus_namespace").(string)),
			RelayName:           &relayName,
			RelayArmURI:         &relayArmURI,
			Hostname:            utils.String(d.Get("hostname").(string)),
			Port:                &port,
			SendKeyName:         utils.String(d.Get("send_key_name").(string)),
			SendKeyValue:        utils.String(d.Get("send_key_value").(string)),
			ServiceBusSuffix:    utils.String(d.Get("service_bus_suffix").(string)),
		},
	}

	hybridConnection, err := client.CreateOrUpdateHybridConnection(ctx, resourceGroup, name, namespaceName, relayName, connectionEnvelope)
	if err != nil {
		return fmt.Errorf("Error creating App Service Hybrid Connection %q (resource group %q): %s", name, resourceGroup, err)
	}

	d.SetId(*hybridConnection.ID)
	return resourceArmAppServiceHybridConnectionRead(d, meta)
}

func resourceArmAppServiceHybridConnectionRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Web.AppServicesClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	name := id.Path["sites"]
	namespaceName := id.Path["hybridConnectionNamespaces"]
	relayName := id.Path["relays"]

	resp, err := client.GetHybridConnection(ctx, resourceGroup, name, namespaceName, relayName)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error making Read request on App Service Hybrid Connection %q in Namespace %q, Resource Group %q: %s", name, namespaceName, resourceGroup, err)
	}
	d.Set("app_service_name", name)
	d.Set("resource_group_name", resourceGroup)
	d.Set("namespace_name", namespaceName)
	d.Set("relay_name", relayName)

	if props := resp.HybridConnectionProperties; props != nil {
		d.Set("port", resp.Port)
		d.Set("service_bus_namespace", resp.ServiceBusNamespace)
		d.Set("send_key_name", resp.SendKeyName)
		d.Set("service_bus_suffix", resp.ServiceBusSuffix)
		d.Set("relay_id", resp.RelayArmURI)
		d.Set("hostname", resp.Hostname)
	}
	// key values are not returned in the response, so we get the primary key from the Service Bus ListKeys func
	serviceBusNSClient := meta.(*clients.Client).ServiceBus.NamespacesClient
	serviceBusNSctx, serviceBusNSCancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer serviceBusNSCancel()

	accessKeys, err := serviceBusNSClient.ListKeys(serviceBusNSctx, resourceGroup, *resp.ServiceBusNamespace, *resp.SendKeyName)
	if err != nil {
		log.Printf("[WARN] Unable to List default keys for Namespace %q (Resource Group %q): %+v", name, resourceGroup, err)
	} else {
		d.Set("send_key_value", accessKeys.PrimaryKey)
	}

	return nil
}

func resourceArmAppServiceHybridConnectionDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Web.AppServicesClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	name := id.Path["sites"]
	namespaceName := id.Path["hybridConnectionNamespaces"]
	relayName := id.Path["relays"]

	resp, err := client.DeleteHybridConnection(ctx, resourceGroup, name, namespaceName, relayName)
	if err != nil {
		if !response.WasNotFound(resp.Response) {
			return nil
		}
		return fmt.Errorf("Error deleting App Service Hybrid Connection %q (Resource Group %q, Relay %q): %+v", name, resourceGroup, relayName, err)
	}

	return nil
}