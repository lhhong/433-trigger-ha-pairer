package server

import (
	"encoding/json"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/puzpuzpuz/xsync"
)

type SourceTriggerMessage struct {
	Id    SourceTriggerId `json:"id"`
	Model string          `json:"model"`
}

type triggerLongHoldState struct {
	triggerHash    int
	firstTriggered time.Time
	sentLongPress  bool
	count          uint
}

type longHoldStates struct {
	triggers map[SourceTriggerId]triggerLongHoldState
	locks    *xsync.MapOf[SourceTriggerId, *sync.Mutex]
}

var longHold longHoldStates = longHoldStates{
	triggers: make(map[SourceTriggerId]triggerLongHoldState),
	locks:    xsync.NewTypedMapOf[SourceTriggerId, *sync.Mutex](func (id SourceTriggerId) uint64 {
		return xsync.StrHash64(string(id))
	}),
}

const rootTopic string = "trigger2mqtt"

var buttonShortPress = "button_short_press"
var buttonLongPress = "button_long_press"
var buttonLongRelease = "button_long_release"

func (devConf *DeviceConfig) toMqttMessages(envVars EnvVars) mqttMessages {
	triggerMap := make(map[SourceTriggerId]triggerMessages)
	for _, device := range devConf.Devices {
		deviceDiscoveryMessage := DeviceDiscoveryMessage{
			Identifiers: device.Id,
			Name:        device.Name,
			Model:       device.Model,
		}
		holdSupported := device.Model == "Brandless remote"
		for _, trigger := range device.Triggers {
			if _, seen := triggerMap[trigger.SourceId]; seen {
				log.Println("Found duplicated trigger sourceId. Only using the first defined value.")
				continue
			}
			var triggerTopic = rootTopic + "/" + trigger.Id
			var triggerDiscoveryMessages = make(map[discoveryTopic]DiscoveryMessage)
			triggerDiscoveryMessages[envVars.HaDiscoveryPrefix+"/device_automation/rtl_433/"+trigger.Id+"_"+buttonShortPress+"/config"] =
				DiscoveryMessage{
					AutomationType: "trigger",
					Type:           buttonShortPress,
					Payload:        &buttonShortPress,
					SubType:        trigger.SubType,
					Topic:          triggerTopic,
					Device:         deviceDiscoveryMessage,
				}
			if holdSupported {
				triggerDiscoveryMessages[envVars.HaDiscoveryPrefix+"/device_automation/rtl_433/"+trigger.Id+"_"+buttonLongPress+"/config"] =
					DiscoveryMessage{
						AutomationType: "trigger",
						Type:           buttonLongPress,
						Payload:        &buttonLongPress,
						SubType:        trigger.SubType,
						Topic:          triggerTopic,
						Device:         deviceDiscoveryMessage,
					}
				triggerDiscoveryMessages[envVars.HaDiscoveryPrefix+"/device_automation/rtl_433/"+trigger.Id+"_"+buttonLongRelease+"/config"] =
					DiscoveryMessage{
						AutomationType: "trigger",
						Type:           buttonLongRelease,
						Payload:        &buttonLongRelease,
						SubType:        trigger.SubType,
						Topic:          triggerTopic,
						Device:         deviceDiscoveryMessage,
					}
			}

			triggerMap[trigger.SourceId] = triggerMessages{
				triggerId:         trigger.Id,
				triggerTopic:      triggerTopic,
				holdSupported:     holdSupported,
				discoveryMessages: triggerDiscoveryMessages,
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
			if !discovery.holdSupported {
				publishMessage(client, discovery, buttonShortPress)
			} else {
				lock, _ := longHold.locks.LoadOrStore(sourceMessage.Id, &sync.Mutex{})
				lock.Lock()
				state, ok := longHold.triggers[sourceMessage.Id]
				var newTriggerState triggerLongHoldState
				if !ok {
					newTriggerState = triggerLongHoldState{
						triggerHash:    rand.Int(),
						firstTriggered: time.Now(),
						count:          1,
					}
				} else {
					shouldSendLongPress := !state.sentLongPress && state.firstTriggered.Add(300*time.Millisecond).Before(time.Now())
					if shouldSendLongPress {
						log.Println("Starting long press")
						publishMessage(client, discovery, buttonLongPress)
					}
					newTriggerState = triggerLongHoldState{
						triggerHash:    rand.Int(),
						firstTriggered: state.firstTriggered,
						count:          state.count + 1,
						sentLongPress:  state.sentLongPress || shouldSendLongPress,
					}
				}
				longHold.triggers[sourceMessage.Id] = newTriggerState
				lock.Unlock()
				go waitLongHold(client, sourceMessage.Id, discovery, newTriggerState.triggerHash)
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

func waitLongHold(client mqtt.Client, sourceId SourceTriggerId, discovery triggerMessages, triggerHash int) {
	time.Sleep(150 * time.Millisecond)
	lock, _ := longHold.locks.Load(sourceId)
	lock.Lock()
	defer lock.Unlock()
	triggerState, ok := longHold.triggers[sourceId]
	if ok && triggerState.triggerHash == triggerHash {
		log.Println("No trigger response after 150 ms")
		if triggerState.sentLongPress {
			publishMessage(client, discovery, buttonLongRelease)
		} else if triggerState.count > 1 {
			publishMessage(client, discovery, buttonShortPress)
		} else {
			log.Println("Only received 1 signal for trigger, ignoring")
		}
		delete(longHold.triggers, sourceId)
	}
}

func publishMessage(client mqtt.Client, triggerMessage triggerMessages, actionType string) {
	token := client.Publish(triggerMessage.triggerTopic, 1, false, actionType)
	if !token.WaitTimeout(1*time.Second) || token.Error() != nil {
		log.Println("Error publishing trigger activation: ", triggerMessage.triggerId)
	} else {
		log.Println("Republished trigger activation to HA: ", triggerMessage.triggerId)
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
		for topic, discovery := range triggerMsg.discoveryMessages {
			log.Println("Publishing discovery: ", topic)
			payload, err := json.Marshal(discovery)
			if err != nil {
				log.Println("Failed to serialize json: ", err)
			}
			tokens = append(tokens, inflightPublish{
				token:     client.Publish(topic, 1, true, payload),
				triggerId: triggerMsg.triggerId,
			})
		}
	}

	for _, token := range tokens {
		if !token.token.WaitTimeout(1*time.Second) || token.token.Error() != nil {
			log.Println("Failed to publish trigger discovery: ", token.triggerId, " ", token.token.Error())
		}
	}

}
