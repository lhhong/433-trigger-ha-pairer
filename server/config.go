package server

import (
	"errors"
	"log"
	"os"
	"path"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/teris-io/shortid"
	"gopkg.in/yaml.v2"
)

type RegisteredDevice struct {
	Id       string              `yml:"id"`
	Name     string              `yml:"name"`
	Triggers []RegisteredTrigger `yml:"triggers"`
}

type RegisteredTrigger struct {
	Id   string `yml:"id"`
	Name string `yml:"name"`
}

type DeviceConfig struct {
	Devices []RegisteredDevice `yml:"devices"`
}

type ConfigState struct {
	configFile string
	DevConf    *DeviceConfig
}

const deviceConfigFileName string = "devices.yml"

func (c *ConfigState) CloneDevConf() DeviceConfig {
	yamlConf, err := yaml.Marshal(c.DevConf)
	if err != nil {
		log.Fatal("Unexpected failure", err)
	}
	var clonedDevConf DeviceConfig
	err = yaml.Unmarshal(yamlConf, &clonedDevConf)
	if err != nil {
		log.Println("Failed to unmarshal device config", err)
	}
	return clonedDevConf
}

func AddDevice(conf ConfigState, deviceName string) (*RegisteredDevice, error) {
	newId := shortid.MustGenerate()
	newDevice := RegisteredDevice{newId, deviceName, []RegisteredTrigger{}}

	clonedDevConf := conf.CloneDevConf()
	clonedDevConf.Devices = append(clonedDevConf.Devices, newDevice)
	writeDeviceConfig(conf.configFile, clonedDevConf)

	err := waitASecond(func() bool {
		return findDevice(*conf.DevConf, newId) != nil
	})
	if err != nil {
		return nil, errors.New("Failed to add new device")
	}
	return findDevice(*conf.DevConf, newId), nil
}

func AddTrigger(conf ConfigState, deviceId string, triggerName string) (*RegisteredTrigger, error) {
	newId := shortid.MustGenerate()
	newTrigger := RegisteredTrigger{newId, triggerName}

	clonedDevConf := conf.CloneDevConf()
	deviceIdx := findDeviceIdx(clonedDevConf, deviceId)
	if deviceIdx < 0 {
		return nil, errors.New("Device not found: " + deviceId)
	}
	clonedDevConf.Devices[deviceIdx].Triggers = append(clonedDevConf.Devices[deviceIdx].Triggers, newTrigger)
	writeDeviceConfig(conf.configFile, clonedDevConf)

	err := waitASecond(func() bool {
		return findTrigger(*conf.DevConf, deviceId, newId) != nil
	})
	if err != nil {
		return nil, errors.New("Failed to add new trigger")
	}
	return findTrigger(*conf.DevConf, deviceId, newId), nil
}

func waitASecond(testComplete func() bool) error {
	for i := 0; i < 20; i++ {
		if testComplete() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("Condition did not turn true")
}

func findDevice(conf DeviceConfig, id string) *RegisteredDevice {
	for _, device := range conf.Devices {
		if device.Id == id {
			return &device
		}
	}
	return nil
}

func findDeviceIdx(conf DeviceConfig, id string) int {
	for idx, device := range conf.Devices {
		if device.Id == id {
			return idx
		}
	}
	return -1
}

func findTrigger(conf DeviceConfig, deviceId string, triggerId string) *RegisteredTrigger {
	device := findDevice(conf, deviceId)
	if device == nil {
		return nil
	}
	for _, trigger := range device.Triggers {
		if trigger.Id == triggerId {
			return &trigger
		}
	}
	return nil
}

func writeDeviceConfig(configFile string, deviceConf DeviceConfig) {
	initialConfig, err := yaml.Marshal(&deviceConf)
	if err != nil {
		log.Fatal("Unexpected failure", err)
	}
	os.WriteFile(configFile, initialConfig, 0666)
}

func InitConfig(watcher *fsnotify.Watcher) ConfigState {
	configDir := os.Getenv("CONFIG_DIR")
	if len(configDir) == 0 {
		configDir = "./config-dev"
	}
	deviceConfigFile := path.Join(configDir, deviceConfigFileName)

	initConfigFileIfNotExist(configDir, deviceConfigFile)

	var config ConfigState = ConfigState{deviceConfigFile, &DeviceConfig{[]RegisteredDevice{}}}

	watchConfigFile(deviceConfigFile, watcher, config.DevConf)

	loadedConf, err := loadExistingConfig(deviceConfigFile)
	if err != nil {
		log.Fatal("Failed initial config load: ", err)
	}
	*(config.DevConf) = *loadedConf

	return config
}

func initConfigFileIfNotExist(configDir string, deviceConfigFile string) {
	_, err := os.Stat(deviceConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		os.MkdirAll(configDir, 0777)
		writeDeviceConfig(deviceConfigFile, DeviceConfig{[]RegisteredDevice{}})
	}
}

func watchConfigFile(deviceConfigFile string, watcher *fsnotify.Watcher, deviceConf *DeviceConfig) {
	err := watcher.Add(deviceConfigFile)
	if err != nil {
		log.Fatal("Unexpected error", err)
	}

	go func(deviceConf *DeviceConfig) {
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
						*deviceConf = *loadedConfig
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
	}(deviceConf)
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
