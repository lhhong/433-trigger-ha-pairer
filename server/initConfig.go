package server

import (
	"errors"
	"log"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
)

func InitConfig(watcher *fsnotify.Watcher) ConfigState {
	envVars := getEnvVars()

	deviceConfigFile := getDeviceConfigFile(envVars)

	initConfigFileIfNotExist(envVars.ConfigDir, deviceConfigFile)

	var config ConfigState = ConfigState{
		EnvVars:      envVars,
		mqttMessages: &mqttMessages{},
		DevConf:      &DeviceConfig{[]Device{}}}

	watchConfigFile(deviceConfigFile, watcher, config)

	loadedConf, err := loadExistingConfig(deviceConfigFile)
	if err != nil {
		log.Fatal("Failed initial config load: ", err)
	}
	mqttMessages := loadedConf.toMqttMessages(envVars)
	*(config.DevConf) = *loadedConf
	*(config.mqttMessages) = *&mqttMessages

	return config
}

func getEnvVars() EnvVars {
	haDiscoveryPrefix := os.Getenv("HA_DISCOVERY_PREFIX")
	if len(haDiscoveryPrefix) == 0 {
		haDiscoveryPrefix = "homeassistant"
	}
	configDir := os.Getenv("CONFIG_DIR")
	if len(configDir) == 0 {
		configDir = "./config-dev"
	}
	mqttBroker := os.Getenv("MQTT_BROKER")
	if len(mqttBroker) == 0 {
		mqttBroker = "mqtt://127.0.0.1:1883"
	}
	return EnvVars{
		HaDiscoveryPrefix: haDiscoveryPrefix,
		ConfigDir:         configDir,
		MqttBroker:        mqttBroker,
	}
}

func initConfigFileIfNotExist(configDir string, deviceConfigFile string) {
	_, err := os.Stat(deviceConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		os.MkdirAll(configDir, 0777)
		writeDeviceConfig(deviceConfigFile, DeviceConfig{[]Device{}})
	}
}

func watchConfigFile(deviceConfigFile string, watcher *fsnotify.Watcher, config ConfigState) {
	err := watcher.Add(deviceConfigFile)
	if err != nil {
		log.Fatal("Unexpected error", err)
	}

	go func(config ConfigState) {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Println("Config file watcher event", event)
				for i := 0; i < 15; i++ {
					// A little sleep time and retries otherwise it throws file not found due to racey conditions
					time.Sleep(30 * time.Millisecond)
					loadedConfig, err := loadExistingConfig(deviceConfigFile)
					if err != nil {
						log.Println("Failed to reload config: ", err)
					} else {
						*config.DevConf = *loadedConfig
						*config.mqttMessages = (*loadedConfig).toMqttMessages(config.EnvVars)
						break
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Error watching file event", err)
			}
		}
	}(config)
}

func loadExistingConfig(deviceConfigFile string) (*DeviceConfig, error) {
	log.Println("Loading config from: ", deviceConfigFile)
	deviceConfLoad, err := os.ReadFile(deviceConfigFile)
	if err != nil {
		log.Println("Failed to read device config", err)
		return nil, err
	}
	var deviceConf DeviceConfig
	err = yaml.Unmarshal(deviceConfLoad, &deviceConf)
	if err != nil {
		log.Println("Failed to unmarshal device config", err)
	}
	return &deviceConf, err
}
