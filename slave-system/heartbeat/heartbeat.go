package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"slave-system/config"
)

var IsMasterDown bool
var failCount int

// StartHeartbeat begins the heartbeat goroutine
func StartHeartbeat() {
	go func() {
		for {
			sendHeartbeat()
			time.Sleep(5 * time.Second)
		}
	}()
}

func sendHeartbeat() {
	// If failover happened and we are now master, stop sending heartbeats
	if config.AppConfig.Role == "master" {
		return
	}

	masterURL := fmt.Sprintf("http://%s:%s/heartbeat", config.AppConfig.MasterIP, config.AppConfig.MasterPort)
	myAddr := fmt.Sprintf("%s:%s", config.AppConfig.IP, config.AppConfig.Port)

	payload := map[string]string{"address": myAddr}
	data, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(masterURL, "application/json", bytes.NewBuffer(data))
	
	if err != nil || resp.StatusCode != http.StatusOK {
		failCount++
		if !IsMasterDown {
			log.Printf("Failed to send heartbeat to master. Fail count: %d\n", failCount)
		}
		if failCount >= 3 && !IsMasterDown {
			IsMasterDown = true
			log.Println("Master is confirmed DOWN! Initiating simple failover...")
			promoteToMaster()
		}
	} else {
		failCount = 0
		if IsMasterDown {
			log.Println("Master is back online!")
		}
		IsMasterDown = false
		resp.Body.Close()
	}
}

func promoteToMaster() {
	log.Println("-------------------------------------------------")
	log.Println("FAILOVER: This Slave is now becoming the MASTER!")
	log.Println("-------------------------------------------------")
	config.AppConfig.Role = "master"
	// For this simple educational demo, we just change our role locally 
	// so that future writes don't get forwarded but are instead handled locally
	// if we added the master handlers here (left out for simplicity).
}
