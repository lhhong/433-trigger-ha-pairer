package server

type triggerMessages struct {
	triggerId        string
	discoveryTopic   string
	discoveryMessage DiscoveryMessage
}

type DiscoveryMessage struct {
	AutomationType string                 `json:"automation_type"` // trigger
	Type           string                 `json:"type"`            // button_short_press
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
