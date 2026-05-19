package heartbeat

import (
	"log"
	"sync"
	"time"
)

var Slaves = make(map[string]time.Time)
var mu sync.Mutex

// RegisterSlave registers a slave or updates its last heartbeat timestamp
func RegisterSlave(slaveAddress string) {
	mu.Lock()
	defer mu.Unlock()
	Slaves[slaveAddress] = time.Now()
}

// GetActiveSlaves returns slaves that sent a heartbeat within the last 15 seconds
func GetActiveSlaves() []string {
	mu.Lock()
	defer mu.Unlock()
	var active []string
	now := time.Now()
	for addr, lastBeat := range Slaves {
		if now.Sub(lastBeat) < 15*time.Second {
			active = append(active, addr)
		} else {
			log.Printf("Slave %s considered inactive (heartbeat timeout)\n", addr)
			delete(Slaves, addr)
		}
	}
	return active
}
