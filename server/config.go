package server

import (
	"errors"
	"log"
	"os"
	"path"
	"time"

	"github.com/teris-io/shortid"
	"gopkg.in/yaml.v2"
)

type Device struct {
	Id       string    `yml:"id"`
	Name     string    `yml:"name"`
	Model    string    `yml:"model"`
	Triggers []Trigger `yml:"triggers"`
}

type SourceTriggerId string

type Trigger struct {
	Id       string          `yml:"id"`
	SourceId SourceTriggerId `yml:"sourceId"`
	SubType  string          `yml:"subType"`
}

type DeviceConfig struct {
	Devices []Device `yml:"devices"`
}

type EnvVars struct {
	HaDiscoveryPrefix string
	ConfigDir         string
	MqttBroker        string
}

type ConfigState struct {
	EnvVars      EnvVars
	mqttMessages *mqttMessages
	DevConf      *DeviceConfig
}

func getDeviceConfigFile(envVars EnvVars) string {
	return path.Join(envVars.ConfigDir, "devices.yml")
}

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

func AddDevice(conf ConfigState, deviceName string) (*Device, error) {
	newId := shortid.MustGenerate()
	newDevice := Device{newId, deviceName, "", []Trigger{}}

	clonedDevConf := conf.CloneDevConf()
	clonedDevConf.Devices = append(clonedDevConf.Devices, newDevice)
	writeDeviceConfig(getDeviceConfigFile(conf.EnvVars), clonedDevConf)

	err := waitASecond(func() bool {
		return findDevice(*conf.DevConf, newId) != nil
	})
	if err != nil {
		return nil, errors.New("Failed to add new device")
	}
	return findDevice(*conf.DevConf, newId), nil
}

func AddTrigger(
	conf ConfigState,
	deviceId string,
	triggerSubType string,
	triggerSourceId SourceTriggerId,
	deviceModel string) (*Trigger, error) {
	newId := shortid.MustGenerate()
	newTrigger := Trigger{
		Id:       newId,
		SourceId: triggerSourceId,
		SubType:  triggerSubType,
	}

	clonedDevConf := conf.CloneDevConf()
	deviceIdx := findDeviceIdx(clonedDevConf, deviceId)
	if deviceIdx < 0 {
		return nil, errors.New("Device not found: " + deviceId)
	}
	clonedDevConf.Devices[deviceIdx].Triggers = append(clonedDevConf.Devices[deviceIdx].Triggers, newTrigger)
	clonedDevConf.Devices[deviceIdx].Model = deviceModel
	writeDeviceConfig(getDeviceConfigFile(conf.EnvVars), clonedDevConf)

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

func findDevice(conf DeviceConfig, id string) *Device {
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

func findTrigger(conf DeviceConfig, deviceId string, triggerId string) *Trigger {
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
