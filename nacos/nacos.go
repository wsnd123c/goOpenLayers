package nacos

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"github.com/nacos-group/nacos-sdk-go/clients"
	"github.com/nacos-group/nacos-sdk-go/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/common/constant"
	"github.com/nacos-group/nacos-sdk-go/vo"
)

var Client *ConfigClient

// Init initializes the Nacos client with the given parameters
func Init(ip, port, nameSpaceId, username, password string) error {
	// Remove leading colon if present
	if len(port) > 0 && port[0] == ':' {
		port = port[1:]
	}
	
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port: %s", port)
	}

	// Create server config
	serverConfigs := []constant.ServerConfig{
		{
			IpAddr: ip,
			Port:   uint64(portInt),
		},
	}

	// Create client config with cross-platform paths
	tempDir := os.TempDir()
	logDir := filepath.Join(tempDir, "nacos", "log")
	cacheDir := filepath.Join(tempDir, "nacos", "cache")
	
	// Ensure directories exist
	os.MkdirAll(logDir, 0755)
	os.MkdirAll(cacheDir, 0755)
	
	clientConfig := constant.ClientConfig{
		NamespaceId:         nameSpaceId,
		TimeoutMs:           5000,
		NotLoadCacheAtStart: true,
		LogDir:              logDir,
		CacheDir:            cacheDir,
		LogLevel:            "debug",
		Username:            username,
		Password:            password,
	}

	// Create config client
	configClient, err := clients.CreateConfigClient(map[string]interface{}{
		"serverConfigs": serverConfigs,
		"clientConfig":  clientConfig,
	})
	if err != nil {
		return fmt.Errorf("failed to create nacos config client: %v", err)
	}

	Client = &ConfigClient{client: configClient}
	return nil
}

// ConfigClient wraps the nacos config client with convenient methods
type ConfigClient struct {
	client config_client.IConfigClient
}

// Get retrieves configuration content from Nacos
func (c *ConfigClient) Get(dataId, group string) (string, error) {
	content, err := c.client.GetConfig(vo.ConfigParam{
		DataId: dataId,
		Group:  group,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get config from nacos: %v", err)
	}
	return content, nil
}

// Listen starts listening for configuration changes
func (c *ConfigClient) Listen(dataId, group string, callback func(string)) error {
	fmt.Printf("🎧 Setting up Nacos listener for dataId=%s, group=%s\n", dataId, group)
	
	err := c.client.ListenConfig(vo.ConfigParam{
		DataId: dataId,
		Group:  group,
		OnChange: func(namespace, group, dataId, data string) {
			fmt.Printf("🔔 Nacos OnChange triggered: namespace=%s, group=%s, dataId=%s\n", namespace, group, dataId)
			fmt.Printf("🔔 New config data: %s\n", data)
			callback(data)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to listen to nacos config: %v", err)
	}
	
	fmt.Println("✅ Nacos listener registered successfully")
	return nil
}
