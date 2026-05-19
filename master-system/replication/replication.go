package replication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"master-system/heartbeat"
)

type ReplicateData struct {
	Action string      `json:"action"` // insert, update, delete
	Table  string      `json:"table"`
	Data   interface{} `json:"data"` // map of column to value
}

// ReplicateToSlaves sends data to all active slaves concurrently using goroutines
func ReplicateToSlaves(action string, table string, data interface{}) {
	currentSlaves := heartbeat.GetActiveSlaves()

	reqData := ReplicateData{
		Action: action,
		Table:  table,
		Data:   data,
	}

	payload, err := json.Marshal(reqData)
	if err != nil {
		log.Printf("Failed to marshal replication data: %v", err)
		return
	}

	// Concurrency: Use a goroutine for each slave to replicate simultaneously
	for _, slaveAddr := range currentSlaves {
		go sendReplication(slaveAddr, payload)
	}
}

func sendReplication(slaveAddr string, payload []byte) {
	url := fmt.Sprintf("http://%s/replicate", slaveAddr)
	
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Failed to replicate to slave %s: %v", slaveAddr, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		log.Printf("Successfully replicated to slave %s", slaveAddr)
	} else {
		log.Printf("Failed to replicate to slave %s, status: %d", slaveAddr, resp.StatusCode)
	}
}
