package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"whatsbridge/internal/bot"
	"whatsbridge/internal/db"
	"time"
)

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if bot.GlobalClient == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"loggedIn":  false,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected": bot.GlobalClient.IsConnected(),
		"loggedIn":  bot.GlobalClient.IsLoggedIn(),
	})
}

func SendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if bot.GlobalClient == nil || !bot.GlobalClient.IsConnected() || !bot.GlobalClient.IsLoggedIn() {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Bot is not connected to WhatsApp",
		})
		return
	}

	err := r.ParseMultipartForm(50 << 20)
	var to, message string
	var fileBytes []byte
	var fileName string

	if err == nil {
		to = r.FormValue("to")
		message = r.FormValue("message")

		file, header, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			fileBytes, _ = io.ReadAll(file)
			fileName = header.Filename
		}
	} else {
		var req struct {
			To      string `json:"to"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Invalid request format",
			})
			return
		}
		to = req.To
		message = req.Message
	}

	if to == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Phone number is required",
		})
		return
	}

	var sendErr error
	if len(fileBytes) > 0 {
		tmpFile := fmt.Sprintf("temp_%s", fileName)
		err = os.WriteFile(tmpFile, fileBytes, 0644)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Internal error saving file"})
			return
		}
		defer os.Remove(tmpFile)
		sendErr = bot.SendMediaMessage(to, tmpFile, message)
	} else {
		if message == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Message is required"})
			return
		}
		sendErr = bot.SendTextMessage(to, message)
	}

	if sendErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to send message: %v", sendErr),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	m, err := db.GetMetrics()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(m)
}

func ScheduleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		To           string `json:"to"`
		Message      string `json:"message"`
		ScheduledFor string `json:"scheduled_for"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Invalid JSON"})
		return
	}

	t, err := time.Parse(time.RFC3339, req.ScheduledFor)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Invalid time format"})
		return
	}

	err = db.AddScheduledMessage(req.To, req.Message, t.UTC().Format(time.RFC3339))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Database error"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func BulkSendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		Messages []struct {
			To      string `json:"to"`
			Message string `json:"message"`
		} `json:"messages"`
		IntervalMs int `json:"interval_ms"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Invalid format"})
		return
	}

	go func() {
		for _, m := range req.Messages {
			if bot.GlobalClient == nil || !bot.GlobalClient.IsConnected() || !bot.GlobalClient.IsLoggedIn() {
				break
			}
			bot.SendTextMessage(m.To, m.Message)

			if req.IntervalMs > 0 {
				time.Sleep(time.Duration(req.IntervalMs) * time.Millisecond)
			}
		}
	}()

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": fmt.Sprintf("Started dispatching %d messages", len(req.Messages))})
}

func QRHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	bot.QRMutex.Lock()
	defer bot.QRMutex.Unlock()

	if bot.CurrentQR == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "No QR code available or already logged in"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"code": bot.CurrentQR})
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if bot.GlobalClient == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Bot not initialized"})
		return
	}

	err := bot.Logout()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func ConnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if bot.GlobalClient == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Bot not initialized"})
		return
	}

	err := bot.GlobalClient.Connect()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func DisconnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if bot.GlobalClient == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Bot not initialized"})
		return
	}

	bot.GlobalClient.Disconnect()
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

