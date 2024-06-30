package server

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type SourceTriggerMessage struct {
	Id    SourceTriggerId `json:"id"`
	Model string          `json:"model"`
}

const rootTopic string = "trigger2mqtt"

func (devConf *DeviceConfig) toMqttMessages(envVars EnvVars) mqttMessages {
	triggerMap := make(map[SourceTriggerId]triggerMessages)
	for _, device := range devConf.Devices {
		deviceDiscoveryMessage := DeviceDiscoveryMessage{
			Identifiers: device.Id,
			Name:        device.Name,
			Model:       device.Model,
		}
		for _, trigger := range device.Triggers {
			if _, seen := triggerMap[trigger.SourceId]; seen {
				log.Println("Found duplicated trigger sourceId. Only using the first defined value.")
				continue
			}

			triggerDiscoveryMessage := DiscoveryMessage{
				AutomationType: "trigger",
				Type:           "button_short_press",
				SubType:        trigger.SubType,
				Topic:          rootTopic + "/" + trigger.Id,
				Device:         deviceDiscoveryMessage,
			}

			triggerMap[trigger.SourceId] = triggerMessages{
				triggerId:        trigger.Id,
				discoveryTopic:   envVars.HaDiscoveryPrefix + "/device_automation/" + trigger.Id + "/config",
				discoveryMessage: triggerDiscoveryMessage,
			}
		}
	}

	return mqttMessages{
		triggers: triggerMap,
	}
}

func InitMqtt(config ConfigState, pairing PairingState) mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.EnvVars.MqttBroker)
	opts.SetClientID("trigger2mqtt")
	opts.SetOrderMatters(false)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); !token.WaitTimeout(1*time.Second) || token.Error() != nil {
		panic(token.Error())
	}
	if token := client.Subscribe("rtl_433/events", 1, rtl433EventHandler(config.mqttMessages, pairing)); !token.WaitTimeout(1*time.Second) || token.Error() != nil {
		log.Println("Failed to Subscribe to rtl_433 events")
	} else {
		log.Println("Subscribed to rtl_433 events")
	}

	PublishAllDiscovery(config, client)

	return client
}

func rtl433EventHandler(mqttRoutes *mqttMessages, pairing PairingState) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {
		var sourceMessage SourceTriggerMessage
		err := json.Unmarshal(msg.Payload(), &sourceMessage)
		if err != nil {
			log.Println("Failed to read rtl_433 event message: ", err, "\nMessage: ", string(msg.Payload()[:]))
		}

		discovery, ok := mqttRoutes.triggers[sourceMessage.Id]
		if ok {
			token := client.Publish(discovery.discoveryMessage.Topic, 1, false, "1")
			if !token.WaitTimeout(1*time.Second) || token.Error() != nil {
				log.Println("Error publishing trigger activation: ", discovery.triggerId)
			} else {
				log.Println("Republished trigger activation to HA,\n  device: ",
					discovery.discoveryMessage.Device.Name, "\n  trigger: ", discovery.discoveryMessage.SubType)
			}
		} else {

			pairingChannel := *pairing.Channel
			if pairingChannel != nil && !pairing.closing.Load() {
				// Pairing in progress, if the trigger don't match existing stuff, send it to the pairing channel
				pairing.sending.Add(1)
				pairingChannel <- sourceMessage
				pairing.sending.Add(-1)
			} else {
				log.Println("Received unmatched device ID: ", sourceMessage.Id)
			}
		}

	}
}

type inflightPublish struct {
	token     mqtt.Token
	triggerId string
}

func PublishAllDiscovery(config ConfigState, client mqtt.Client) {

	client.AddRoute(config.EnvVars.HaDiscoveryPrefix+"/device_automation/#/config", func(c mqtt.Client, msg mqtt.Message) {
		log.Println("Existing triggers: ", strings.Split(msg.Topic(), "/")[2])
		// TODO: Remove existing triggers that doesn't exist in config anymore. Can be done by sending 0 byte message to the topic.
	})

	tokens := make([]inflightPublish, 0)
	for _, triggerMsg := range config.mqttMessages.triggers {
		log.Println("Publishing discovery: ", triggerMsg.discoveryTopic)
		payload, err := json.Marshal(triggerMsg.discoveryMessage)
		if err != nil {
			log.Println("Failed to serialize json: ", err)
		}
		tokens = append(tokens, inflightPublish{
			token:     client.Publish(triggerMsg.discoveryTopic, 1, true, payload),
			triggerId: triggerMsg.triggerId,
		})
	}

	for _, token := range tokens {
		if !token.token.WaitTimeout(1*time.Second) || token.token.Error() != nil {
			log.Println("Failed to publish trigger discovery: ", token.triggerId, " ", token.token.Error())
		}
	}

}
