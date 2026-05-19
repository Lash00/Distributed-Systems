package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
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
			ipAddress := config.AppConfig.IP
			if ipAddress == "0.0.0.0" || ipAddress == "" {
				ipAddress = GetOutboundIP()
			}
			myAddr := fmt.Sprintf("%s:%s", ipAddress, config.AppConfig.Port)
			log.Printf("[ELECTION] Resolved our own network address as: %s\n", myAddr)

			isOtherMaster := false
			activeSlaves := []string{myAddr} // We are active

			log.Printf("[ELECTION] Configured sibling slave nodes list: %v\n", config.AppConfig.SlaveNodes)

			for _, node := range config.AppConfig.SlaveNodes {
				if node == myAddr || node == fmt.Sprintf("127.0.0.1:%s", config.AppConfig.Port) || node == fmt.Sprintf("0.0.0.0:%s", config.AppConfig.Port) {
					log.Printf("[ELECTION] Skipping sibling node (it is ourselves): %s\n", node)
					continue
				}

				statusURL := fmt.Sprintf("http://%s/status", node)
				log.Printf("[ELECTION] Pinging sibling status at: %s\n", statusURL)
				statusResp, err := client.Get(statusURL)
				if err == nil {
					defer statusResp.Body.Close()
					var status struct {
						Role string `json:"role"`
					}
					if err := json.NewDecoder(statusResp.Body).Decode(&status); err == nil {
						log.Printf("[ELECTION] Sibling %s is ALIVE. Role: %s\n", node, status.Role)
						activeSlaves = append(activeSlaves, node)
						if status.Role == "master" {
							isOtherMaster = true
						}
					} else {
						log.Printf("[ELECTION] Failed to decode status response from sibling %s: %v\n", node, err)
					}
				} else {
					log.Printf("[ELECTION] Sibling %s is UNREACHABLE: %v\n", node, err)
				}
			}

			if isOtherMaster {
				log.Println("Failover: Another Slave is already promoted to Master. We will remain a Slave.")
			} else {
				log.Printf("[ELECTION] All currently active slaves: %v\n", activeSlaves)
				// Sort the active slave addresses lexicographically (alphabetically)
				sort.Strings(activeSlaves)
				log.Printf("[ELECTION] Active slaves sorted: %v\n", activeSlaves)

				// Sibling node with the lowest address gets promoted to Master!
				if len(activeSlaves) > 0 && activeSlaves[0] == myAddr {
					log.Printf("[ELECTION] Promoting ourselves! We are the lowest active address (%s == %s)\n", activeSlaves[0], myAddr)
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

// GetOutboundIP dynamically determines the local IP address used to reach the Master
func GetOutboundIP() string {
	conn, err := net.Dial("udp", fmt.Sprintf("%s:%s", config.AppConfig.MasterIP, config.AppConfig.MasterPort))
	if err != nil {
		log.Printf("[WARNING] Could not resolve outbound IP, falling back to localhost: %v", err)
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
