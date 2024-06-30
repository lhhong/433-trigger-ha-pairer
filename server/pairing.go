package server

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type PairingState struct {
	Channel         *chan SourceTriggerMessage
	sending         *atomic.Int32
	closing         *atomic.Bool // Sent by receiver
	lock            *sync.Mutex
}

func InitPairing() PairingState {
	var emptyChan chan SourceTriggerMessage
	return PairingState{
		Channel:         &emptyChan,
		sending:         &atomic.Int32{},
		closing:         &atomic.Bool{},
		lock:            &sync.Mutex{},
	}
}

func createPairingChannel(pairing PairingState) bool {
	if *(pairing.Channel) != nil {
		return false
	}
	if !pairing.lock.TryLock() {
		return false
	}
	*(pairing.Channel) = make(chan SourceTriggerMessage)
	return true
}

func resetPairing(pairing PairingState) {
	pairing.closing.Store(true)
	for range pairing.sending.Load() {
		// Clear out the sending queue
		_ = <-*pairing.Channel
	}

	close(*pairing.Channel)
	if pairing.sending.Load() != 0 {
		log.Println("Why are there still stuff sending?")
	}
	*(pairing.Channel) = nil
	pairing.closing.Store(false)
	pairing.lock.Unlock()
}

type triggerTracker struct {
	deviceModel      string
	consecutiveCount uint8
	lastTriggered    time.Time
}

func StartPairing(deviceId string, triggerSubType string, config ConfigState, pairing PairingState) (*Trigger, error) {
	device := findDevice(*config.DevConf, deviceId)
	if device == nil {
		return nil, errors.New("Device not found: " + deviceId)
	}

	success := createPairingChannel(pairing)
	if !success {
		return nil, errors.New("Another pairing in progress")
	}
	defer resetPairing(pairing)

	startClosing := make(chan bool)

	trackers := make(map[SourceTriggerId]triggerTracker)
	var selectedTracker *SourceTriggerId

	go func() {
		time.Sleep(20 * time.Second)
		startClosing <- true
	}()

	loop:
	for {
		select {
		case trigger := <-*pairing.Channel:
			if len(device.Model) != 0 && device.Model != trigger.Model {
				log.Println("Ignoring trigger due to model mismatch")
				continue loop
			}
			tracked, ok := trackers[trigger.Id]
			if !ok {
				trackers[trigger.Id] = triggerTracker{
					deviceModel:      trigger.Model,
					consecutiveCount: 1,
					lastTriggered:    time.Now(),
				}
				continue loop
			}
			if time.Now().Sub(tracked.lastTriggered) > 2*time.Second {
				log.Println("Took too long for subsequent press")
				trackers[trigger.Id] = triggerTracker{
					deviceModel:      trigger.Model,
					consecutiveCount: 1,
					lastTriggered:    time.Now(),
				}
				continue loop
			}
			trackers[trigger.Id] = triggerTracker{
				deviceModel:      trigger.Model,
				consecutiveCount: tracked.consecutiveCount + 1,
				lastTriggered:    time.Now(),
			}
			if trackers[trigger.Id].consecutiveCount < 3 {
				continue loop
			}
			selectedTracker = &trigger.Id
			break loop

		case <-startClosing:
			break loop
		}
	}

	if selectedTracker == nil {
		return nil, errors.New("No triggers paired")
	}

	trigger, _ := trackers[*selectedTracker]
	return AddTrigger(config, deviceId, triggerSubType, *selectedTracker, trigger.deviceModel)
}
