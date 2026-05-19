package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
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

			// Master Selection: check configured sibling slaves to prevent split-brain
			myAddr := fmt.Sprintf("%s:%s", config.AppConfig.IP, config.AppConfig.Port)
			if config.AppConfig.IP == "0.0.0.0" || config.AppConfig.IP == "" {
				myAddr = fmt.Sprintf("127.0.0.1:%s", config.AppConfig.Port)
			}

			isOtherMaster := false
			activeSlaves := []string{myAddr} // We are active

			for _, node := range config.AppConfig.SlaveNodes {
				if node == myAddr || node == fmt.Sprintf("127.0.0.1:%s", config.AppConfig.Port) {
					continue // Skip ourselves
				}

				statusURL := fmt.Sprintf("http://%s/status", node)
				statusResp, err := client.Get(statusURL)
				if err == nil {
					defer statusResp.Body.Close()
					var status struct {
						Role string `json:"role"`
					}
					if err := json.NewDecoder(statusResp.Body).Decode(&status); err == nil {
						activeSlaves = append(activeSlaves, node)
						if status.Role == "master" {
							isOtherMaster = true
						}
					}
				}
			}

			if isOtherMaster {
				log.Println("Failover: Another Slave is already promoted to Master. We will remain a Slave.")
			} else {
				// Sort the active slave addresses lexicographically (alphabetically)
				sort.Strings(activeSlaves)

				// Sibling node with the lowest address gets promoted to Master!
				if len(activeSlaves) > 0 && activeSlaves[0] == myAddr {
					promoteToMaster()
				} else {
					log.Printf("Failover: Sibling Slave %s has higher priority. We remain a Slave.\n", activeSlaves[0])
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
