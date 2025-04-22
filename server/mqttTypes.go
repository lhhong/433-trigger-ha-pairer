package server

type discoveryTopic = string

type triggerMessages struct {
	triggerId         string
	holdSupported     bool
	triggerTopic      string
	discoveryMessages map[discoveryTopic]DiscoveryMessage
}

type DiscoveryMessage struct {
	AutomationType string                 `json:"automation_type"` // trigger
	Type           string                 `json:"type"`            // button_short_press
	Payload        *string `json:"payload"`
	SubType        string                 `json:"subtype"`
	Topic          string                 `json:"topic"`
	Device         DeviceDiscoveryMessage `json:"device"`
}

type DeviceDiscoveryMessage struct {
	Identifiers string `json:"identifiers"` // This can be an array in specs
	Name        string `json:"name"`
	Model       string `json:"model"`
}

type mqttMessages struct {
	triggers map[SourceTriggerId]triggerMessages
}
