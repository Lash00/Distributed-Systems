package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"slave-system/config"
	"slave-system/database"
	"slave-system/heartbeat"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load config
	err := config.LoadConfig("config/config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Connect to Database
	database.ConnectDB()

	// Start Heartbeat
	heartbeat.StartHeartbeat()

	// Setup Gin Router
	r := gin.Default()
	r.Use(cors.Default())

	// Serve Frontend
	r.Static("/gui", "./frontend")

	// API Endpoints
	r.GET("/clients", getClients)
	
	// Replicate endpoint called by Master
	r.POST("/replicate", handleReplication)
	
	// Forward write requests to Master
	r.POST("/insert", forwardRequest)
	r.PUT("/update", forwardRequest)
	r.DELETE("/delete", forwardRequest)
	r.POST("/submit-request", forwardRequest)
	
	// Status
	r.GET("/status", getStatus)
	r.GET("/pending-requests", getPendingRequests)
	r.POST("/approve-request", approveChangeRequest)
	r.POST("/reject-request", rejectChangeRequest)

	addr := fmt.Sprintf("%s:%s", config.AppConfig.IP, config.AppConfig.Port)
	log.Printf("Slave Node starting on %s...", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func getClients(c *gin.Context) {
	clients, err := database.GetAllClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, clients)
}

func handleReplication(c *gin.Context) {
	var req struct {
		Action string                 `json:"action"`
		Table  string                 `json:"table"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	log.Printf("Received replication action: %s on table: %s", req.Action, req.Table)

	if req.Table == "clients" {
		switch req.Action {
		case "insert":
			id := int(req.Data["id"].(float64))
			name := req.Data["name"].(string)
			balance := req.Data["balance"].(float64)
			city := req.Data["city"].(string)

			_, err := database.DB.Exec("INSERT INTO clients (id, name, balance, city) VALUES (?, ?, ?, ?)", id, name, balance, city)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		case "update":
			id := int(req.Data["id"].(float64))
			name := req.Data["name"].(string)
			balance := req.Data["balance"].(float64)
			city := req.Data["city"].(string)

			_, err := database.DB.Exec("UPDATE clients SET name=?, balance=?, city=? WHERE id=?", name, balance, city, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		case "delete":
			id := int(req.Data["id"].(float64))

			_, err := database.DB.Exec("DELETE FROM clients WHERE id=?", id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Replication successful"})
}

type ChangeRequest struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Status    string                 `json:"status"`
	OriginIP  string                 `json:"origin_ip"`
	Timestamp string                 `json:"timestamp"`
}

var PendingRequests = []ChangeRequest{}
var reqMu sync.Mutex

func forwardRequest(c *gin.Context) {
	if config.AppConfig.Role == "master" {
		if c.Request.URL.Path == "/submit-request" {
			handleSubmitRequest(c)
		} else {
			handleLocalWrite(c)
		}
		return
	}

	masterURL := fmt.Sprintf("http://%s:%s%s", config.AppConfig.MasterIP, config.AppConfig.MasterPort, c.Request.URL.Path)

	// If the original Master is down, find the promoted Slave Master!
	if heartbeat.IsMasterDown {
		client := &http.Client{Timeout: 1 * time.Second}
		for _, node := range config.AppConfig.SlaveNodes {
			// Query each configured sibling
			statusURL := fmt.Sprintf("http://%s/status", node)
			statusResp, err := client.Get(statusURL)
			if err == nil {
				defer statusResp.Body.Close()
				var status struct {
					Role string `json:"role"`
				}
				if err := json.NewDecoder(statusResp.Body).Decode(&status); err == nil && status.Role == "master" {
					masterURL = fmt.Sprintf("http://%s%s", node, c.Request.URL.Path)
					log.Printf("[FAILOVER ROUTE] Original Master is down. Routing proposal to promoted Slave Master at %s", masterURL)
					break
				}
			}
		}
	}

	log.Printf("Forwarding %s request to %s", c.Request.Method, masterURL)

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	req, err := http.NewRequest(c.Request.Method, masterURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create forward request"})
		return
	}
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to forward request: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
}

func getStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"role":        config.AppConfig.Role,
		"master_down": heartbeat.IsMasterDown,
	})
}

func handleLocalWrite(c *gin.Context) {
	var req struct {
		Type string                 `json:"type"`
		Data map[string]interface{} `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid local write payload: " + err.Error()})
		return
	}

	if req.Type == "insert" {
		name := req.Data["name"].(string)
		balance := req.Data["balance"].(float64)
		city := req.Data["city"].(string)

		if err := ValidateClientData(name, balance, city); err != nil {
			log.Printf("[ERROR] Local insert validation failed: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		res, err := database.DB.Exec("INSERT INTO clients (name, balance, city) VALUES (?, ?, ?)", name, balance, city)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local insert: " + err.Error()})
			return
		}
		id, _ := res.LastInsertId()

		// Retrieve record to replicate
		var client struct {
			ID      int     `json:"id"`
			Name    string  `json:"name"`
			Balance float64 `json:"balance"`
			City    string  `json:"city"`
		}
		_ = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)

		// Replicate to other Slaves
		replicateToOtherSlaves("insert", "clients", client)

		c.JSON(http.StatusOK, gin.H{"message": "Executed directly on promoted Master", "id": fmt.Sprintf("REQ-LOCAL-%d", id)})
		return

	} else if req.Type == "update" {
		id := int(req.Data["id"].(float64))
		name := req.Data["name"].(string)
		balance := req.Data["balance"].(float64)
		city := req.Data["city"].(string)

		if err := ValidateClientData(name, balance, city); err != nil {
			log.Printf("[ERROR] Local update validation failed: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		res, err := database.DB.Exec("UPDATE clients SET name=?, balance=?, city=? WHERE id=?", name, balance, city, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local update: " + err.Error()})
			return
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found in database", id)})
			return
		}

		// Retrieve record to replicate
		var client struct {
			ID      int     `json:"id"`
			Name    string  `json:"name"`
			Balance float64 `json:"balance"`
			City    string  `json:"city"`
		}
		_ = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)

		// Replicate to other Slaves
		replicateToOtherSlaves("update", "clients", client)

		c.JSON(http.StatusOK, gin.H{"message": "Executed directly on promoted Master", "id": "REQ-LOCAL-UPDATE"})
		return

	} else if req.Type == "smart_query" {
		queryText := req.Data["query"].(string)
		text := strings.ToLower(strings.TrimSpace(queryText))

		if strings.HasPrefix(text, "withdraw") {
			amount, id, err := parseWithdrawQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse withdraw query: " + err.Error()})
				return
			}

			var client struct {
				ID      int     `json:"id"`
				Name    string  `json:"name"`
				Balance float64 `json:"balance"`
				City    string  `json:"city"`
			}
			err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			if client.Balance < amount {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Insufficient balance. Client %d has $%0.2f, cannot withdraw $%0.2f.", id, client.Balance, amount)})
				return
			}

			newBalance := client.Balance - amount
			_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local withdrawal: " + err.Error()})
				return
			}

			// Retrieve record to replicate
			client.Balance = newBalance
			replicateToOtherSlaves("update", "clients", client)

			c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Withdrew $%0.2f from Client %d. New balance: $%0.2f.", amount, id, newBalance), "id": "REQ-LOCAL-WITHDRAW"})
			return

		} else if strings.HasPrefix(text, "deposit") {
			amount, id, err := parseDepositQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse deposit query: " + err.Error()})
				return
			}

			var client struct {
				ID      int     `json:"id"`
				Name    string  `json:"name"`
				Balance float64 `json:"balance"`
				City    string  `json:"city"`
			}
			err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			newBalance := client.Balance + amount
			_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local deposit: " + err.Error()})
				return
			}

			// Retrieve record to replicate
			client.Balance = newBalance
			replicateToOtherSlaves("update", "clients", client)

			c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Deposited $%0.2f to Client %d. New balance: $%0.2f.", amount, id, newBalance), "id": "REQ-LOCAL-DEPOSIT"})
			return

		} else if strings.HasPrefix(text, "delete client") {
			id, err := parseDeleteQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse delete query: " + err.Error()})
				return
			}

			res, err := database.DB.Exec("DELETE FROM clients WHERE id=?", id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local delete: " + err.Error()})
				return
			}
			rowsAffected, _ := res.RowsAffected()
			if rowsAffected == 0 {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			// Replicate to other Slaves
			replicateToOtherSlaves("delete", "clients", map[string]interface{}{"id": id})

			c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Deleted client %d successfully.", id), "id": "REQ-LOCAL-DELETE"})
			return
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported smart query format."})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported write request type."})
		return
	}
}

func parseWithdrawQuery(text string) (float64, int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	parts := strings.Split(text, " ")
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("query too short")
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid withdrawal amount: %s", parts[1])
	}

	var id int
	var foundID bool
	for i := len(parts) - 1; i >= 0; i-- {
		val, err := strconv.Atoi(parts[i])
		if err == nil {
			id = val
			foundID = true
			break
		}
	}

	if !foundID {
		return 0, 0, fmt.Errorf("could not locate client ID")
	}

	return amount, id, nil
}

func parseDepositQuery(text string) (float64, int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	parts := strings.Split(text, " ")
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("query too short")
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid deposit amount: %s", parts[1])
	}

	var id int
	var foundID bool
	for i := len(parts) - 1; i >= 0; i-- {
		val, err := strconv.Atoi(parts[i])
		if err == nil {
			id = val
			foundID = true
			break
		}
	}

	if !foundID {
		return 0, 0, fmt.Errorf("could not locate client ID")
	}

	return amount, id, nil
}

func parseDeleteQuery(text string) (int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	parts := strings.Split(text, " ")
	if len(parts) < 2 {
		return 0, fmt.Errorf("query too short")
	}

	for i := len(parts) - 1; i >= 0; i-- {
		val, err := strconv.Atoi(parts[i])
		if err == nil {
			return val, nil
		}
	}
	return 0, fmt.Errorf("could not locate client ID")
}

func replicateToOtherSlaves(action string, table string, data interface{}) {
	allSlaves := []string{"127.0.0.1:8081", "127.0.0.1:8082"}
	myAddr := fmt.Sprintf("%s:%s", config.AppConfig.IP, config.AppConfig.Port)

	type ReplicateData struct {
		Action string      `json:"action"`
		Table  string      `json:"table"`
		Data   interface{} `json:"data"`
	}

	reqData := ReplicateData{
		Action: action,
		Table:  table,
		Data:   data,
	}

	payload, err := json.Marshal(reqData)
	if err != nil {
		log.Printf("[REPLICATION] Failed to marshal replication data: %v", err)
		return
	}

	for _, slaveAddr := range allSlaves {
		if slaveAddr == myAddr {
			continue // Skip ourselves!
		}

		go func(addr string) {
			url := fmt.Sprintf("http://%s/replicate", addr)
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
			if err != nil {
				log.Printf("[REPLICATION] Failed to replicate to other slave %s: %v", addr, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				log.Printf("[REPLICATION] Successfully replicated write to other slave %s", addr)
			} else {
				log.Printf("[REPLICATION] Failed to replicate to other slave %s: status %d", addr, resp.StatusCode)
			}
		}(slaveAddr)
	}
}

func handleSubmitRequest(c *gin.Context) {
	var req struct {
		Type string                 `json:"type"`
		Data map[string]interface{} `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload: " + err.Error()})
		return
	}

	// Validate data contents if inserting/updating
	if req.Type == "insert" || req.Type == "update" {
		name, _ := req.Data["name"].(string)
		var balance float64
		if bVal, ok := req.Data["balance"]; ok {
			switch v := bVal.(type) {
			case float64:
				balance = v
			case int:
				balance = float64(v)
			}
		}
		city, _ := req.Data["city"].(string)
		if err := ValidateClientData(name, balance, city); err != nil {
			log.Printf("[ERROR] Proposal validation failed: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation failed: " + err.Error()})
			return
		}
	}

	// Validate client ID existence for update/smart_query
	if req.Type == "update" {
		idVal, ok := req.Data["id"]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Client ID is required for update"})
			return
		}
		var targetID int
		switch v := idVal.(type) {
		case float64:
			targetID = int(v)
		case int:
			targetID = v
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Client ID format"})
			return
		}

		var exists bool
		err := database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM clients WHERE id=?)", targetID).Scan(&exists)
		if err != nil || !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d does not exist in database", targetID)})
			return
		}
	} else if req.Type == "smart_query" {
		queryText, ok := req.Data["query"].(string)
		if ok {
			text := strings.ToLower(strings.TrimSpace(queryText))
			var targetID int
			var err error
			var isWithdraw, isDeposit, isDelete bool

			if strings.HasPrefix(text, "withdraw") {
				_, targetID, err = parseWithdrawQuery(text)
				isWithdraw = true
			} else if strings.HasPrefix(text, "deposit") {
				_, targetID, err = parseDepositQuery(text)
				isDeposit = true
			} else if strings.HasPrefix(text, "delete client") {
				targetID, err = parseDeleteQuery(text)
				isDelete = true
			}

			if (isWithdraw || isDeposit || isDelete) {
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid query syntax: " + err.Error()})
					return
				}
				var exists bool
				dbErr := database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM clients WHERE id=?)", targetID).Scan(&exists)
				if dbErr != nil || !exists {
					c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d does not exist in database", targetID)})
					return
				}
			}
		}
	}

	reqMu.Lock()
	defer reqMu.Unlock()

	id := fmt.Sprintf("REQ-TEMP-%d", time.Now().UnixNano())
	newReq := ChangeRequest{
		ID:        id,
		Type:      req.Type,
		Data:      req.Data,
		Status:    "pending",
		OriginIP:  c.ClientIP(),
		Timestamp: time.Now().Format("15:04:05"),
	}

	PendingRequests = append(PendingRequests, newReq)
	log.Printf("[INFO] Temporary Master received new change request: %s from %s (Type: %s)", id, newReq.OriginIP, newReq.Type)

	c.JSON(http.StatusOK, gin.H{"message": "Request submitted to Temporary Master successfully", "id": id})
}

func getPendingRequests(c *gin.Context) {
	reqMu.Lock()
	defer reqMu.Unlock()
	c.JSON(http.StatusOK, PendingRequests)
}

func rejectChangeRequest(c *gin.Context) {
	var body struct {
		ID string `json:"id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	reqMu.Lock()
	defer reqMu.Unlock()

	var targetIndex = -1
	for i, r := range PendingRequests {
		if r.ID == body.ID {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
		return
	}

	PendingRequests = append(PendingRequests[:targetIndex], PendingRequests[targetIndex+1:]...)
	log.Printf("[INFO] Temporary Master rejected change request: %s", body.ID)
	c.JSON(http.StatusOK, gin.H{"message": "Request rejected successfully"})
}

func approveChangeRequest(c *gin.Context) {
	var body struct {
		ID string `json:"id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	reqMu.Lock()
	defer reqMu.Unlock()

	var targetReq *ChangeRequest
	var targetIndex = -1
	for i, r := range PendingRequests {
		if r.ID == body.ID {
			targetReq = &PendingRequests[i]
			targetIndex = i
			break
		}
	}

	if targetReq == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
		return
	}

	// Process write locally and replicate downstream to other active Slaves!
	if targetReq.Type == "insert" {
		name := targetReq.Data["name"].(string)
		balance := targetReq.Data["balance"].(float64)
		city := targetReq.Data["city"].(string)

		res, err := database.DB.Exec("INSERT INTO clients (name, balance, city) VALUES (?, ?, ?)", name, balance, city)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write to local DB: " + err.Error()})
			return
		}
		id, _ := res.LastInsertId()

		// Replicate to other Slaves
		var client struct {
			ID      int     `json:"id"`
			Name    string  `json:"name"`
			Balance float64 `json:"balance"`
			City    string  `json:"city"`
		}
		_ = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
		replicateToOtherSlaves("insert", "clients", client)

	} else if targetReq.Type == "update" {
		id := int(targetReq.Data["id"].(float64))
		name := targetReq.Data["name"].(string)
		balance := targetReq.Data["balance"].(float64)
		city := targetReq.Data["city"].(string)

		res, err := database.DB.Exec("UPDATE clients SET name=?, balance=?, city=? WHERE id=?", name, balance, city, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write to local DB: " + err.Error()})
			return
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found in database", id)})
			return
		}

		// Replicate to other Slaves
		var client struct {
			ID      int     `json:"id"`
			Name    string  `json:"name"`
			Balance float64 `json:"balance"`
			City    string  `json:"city"`
		}
		_ = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
		replicateToOtherSlaves("update", "clients", client)

	} else if targetReq.Type == "smart_query" {
		queryText := targetReq.Data["query"].(string)
		text := strings.ToLower(strings.TrimSpace(queryText))

		if strings.HasPrefix(text, "withdraw") {
			amount, id, err := parseWithdrawQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse withdraw query: " + err.Error()})
				return
			}

			var client struct {
				ID      int     `json:"id"`
				Name    string  `json:"name"`
				Balance float64 `json:"balance"`
				City    string  `json:"city"`
			}
			err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			if client.Balance < amount {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Insufficient balance. Client %d has $%0.2f, cannot withdraw $%0.2f.", id, client.Balance, amount)})
				return
			}

			newBalance := client.Balance - amount
			_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local withdrawal: " + err.Error()})
				return
			}

			// Replicate to other Slaves
			client.Balance = newBalance
			replicateToOtherSlaves("update", "clients", client)

		} else if strings.HasPrefix(text, "deposit") {
			amount, id, err := parseDepositQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse deposit query: " + err.Error()})
				return
			}

			var client struct {
				ID      int     `json:"id"`
				Name    string  `json:"name"`
				Balance float64 `json:"balance"`
				City    string  `json:"city"`
			}
			err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			newBalance := client.Balance + amount
			_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local deposit: " + err.Error()})
				return
			}

			// Replicate to other Slaves
			client.Balance = newBalance
			replicateToOtherSlaves("update", "clients", client)

		} else if strings.HasPrefix(text, "delete client") {
			id, err := parseDeleteQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse delete query: " + err.Error()})
				return
			}

			res, err := database.DB.Exec("DELETE FROM clients WHERE id=?", id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute local delete: " + err.Error()})
				return
			}
			rowsAffected, _ := res.RowsAffected()
			if rowsAffected == 0 {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			// Replicate to other Slaves
			replicateToOtherSlaves("delete", "clients", map[string]interface{}{"id": id})
		}
	}

	PendingRequests = append(PendingRequests[:targetIndex], PendingRequests[targetIndex+1:]...)
	log.Printf("[INFO] Temporary Master approved and executed change request: %s", body.ID)
	c.JSON(http.StatusOK, gin.H{"message": "Request approved and executed successfully"})
}

// ValidateClientData checks business rules before writing to database
func ValidateClientData(name string, balance float64, city string) error {
	if balance < 0 {
		return fmt.Errorf("balance cannot be negative ($%0.2f)", balance)
	}

	for _, r := range city {
		if unicode.IsDigit(r) {
			return fmt.Errorf("city name '%s' cannot contain numeric digits", city)
		}
	}
	return nil
}
