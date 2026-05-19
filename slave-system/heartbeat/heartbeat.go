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
	masterURL := fmt.Sprintf("http://%s:%s/heartbeat", config.AppConfig.MasterIP, config.AppConfig.MasterPort)
	myAddr := fmt.Sprintf("%s:%s", config.AppConfig.IP, config.AppConfig.Port)

	payload := map[string]string{"address": myAddr}
	data, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(masterURL, "application/json", bytes.NewBuffer(data))
	
	if err != nil || resp.StatusCode != http.StatusOK {
		// Master is unreachable or returned error
		if config.AppConfig.Role == "master" {
			// We are already the temporary Master. Let's keep checking if the main master comes back.
			failCount++
			return
		}

		failCount++
		if !IsMasterDown {
			log.Printf("Failed to send heartbeat to master. Fail count: %d\n", failCount)
		}
		if failCount >= 3 && !IsMasterDown {
			IsMasterDown = true
			log.Println("Master is confirmed DOWN! Initiating failover master selection...")

			// Master Selection: check the other slave to prevent split-brain
			otherSlavePort := "8082"
			if config.AppConfig.Port == "8082" {
				otherSlavePort = "8081"
			}
			otherStatusURL := fmt.Sprintf("http://127.0.0.1:%s/status", otherSlavePort)

			isOtherMaster := false
			statusResp, err := client.Get(otherStatusURL)
			if err == nil {
				defer statusResp.Body.Close()
				var status struct {
					Role string `json:"role"`
				}
				if err := json.NewDecoder(statusResp.Body).Decode(&status); err == nil {
					if status.Role == "master" {
						isOtherMaster = true
					}
				}
			}

			if isOtherMaster {
				log.Printf("Failover: Other Slave %s is already promoted to Master. We will remain a Slave.\n", otherSlavePort)
			} else {
				// Priority check: lower port number wins (8081 wins over 8082)
				if config.AppConfig.Port == "8081" {
					promoteToMaster()
				} else {
					// We are Slave 2 (8082). We wait a moment to let 8081 promote.
					time.Sleep(2 * time.Second)
					statusResp2, err2 := client.Get("http://127.0.0.1:8081/status")
					if err2 == nil {
						defer statusResp2.Body.Close()
						var status struct {
							Role string `json:"role"`
						}
						if err := json.NewDecoder(statusResp2.Body).Decode(&status); err == nil && status.Role == "master" {
							log.Println("Failover: Slave 1 (8081) promoted successfully. We remain a Slave.")
						} else {
							promoteToMaster()
						}
					} else {
						promoteToMaster()
					}
				}
			}
		}
	} else {
		resp.Body.Close()
		failCount = 0
		if IsMasterDown || config.AppConfig.Role == "master" {
			log.Println("Master is back online! Demoting self to SLAVE...")
			config.AppConfig.Role = "slave"
		}
		IsMasterDown = false
	}
}

func promoteToMaster() {
	log.Println("-------------------------------------------------")
	log.Println("FAILOVER: This Slave is now becoming the MASTER!")
	log.Println("-------------------------------------------------")
	config.AppConfig.Role = "master"
}
